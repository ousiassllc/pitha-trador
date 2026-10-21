// Package paperresultsmcp is the MCP endpoint that serves Paper results from
// the application's SQLite to a remote Grok Bot (issue #783;
// docs/architecture/paper-results-mcp.md), lets it read and change the
// whitelisted basic settings (issues #806, #807) and hand in Paper order
// proposals, which are only recorded here (issue #827).
//
// Transport: MCP "Streamable HTTP" implemented as a small JSON-RPC 2.0
// handler (POST only, every response a single application/json body). It
// has nine read tools (tools.go), the settings tools (settings_tools.go) and
// the order-proposal tools (order_proposal_tools.go, issue #827). The only
// write path is mcpset.Writer: a whitelisted runtime_settings upsert plus its
// audit row, the write switch, one order_proposals row plus its first event,
// and the order-proposal switch turned off. The read connection is mode=ro
// with query_only=ON and the dispatch table is the only way to SQL. Every
// request - initialize and tools/list included - needs the Bearer API key, checked in constant time before any
// SQL runs or any database connection is opened.
package paperresultsmcp

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/readdb"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// Limits fixed by the API specification (§2).
const (
	// MaxConcurrent is the number of tool calls that may run (or wait for
	// the single connection) at once; the next one is concurrency_limited.
	MaxConcurrent = 2
	// QueryTimeout bounds one tool call, lock and connection waiting included.
	QueryTimeout = 3 * time.Second
)

// Log messages. The first is fixed by architecture §3.3.
const (
	logNoAuth = "paper-results-mcp: refusing to listen: no auth configured"
)

// ErrNotListening is returned by Listen when the configuration says not to
// bind (disabled, no API key, a listen address outside §3.1, ...). Nothing
// has been bound in that case.
var ErrNotListening = errors.New("paper-results-mcp: not listening")

// Options configures a Server.
type Options struct {
	// Settings is the local settings file's content.
	Settings settings.Settings
	// DBPath is the application database file; it is opened lazily, read-only.
	DBPath string
	// CapitalJPY is the Paper initial capital for the drawdown ratio; zero or
	// negative leaves the ratio null.
	CapitalJPY float64
	// PaperLimits is config/risk.yaml's Paper section: the default the Paper
	// settings keys show while nothing is stored. Nil uses the issue #804
	// initial values.
	PaperLimits *config.RiskLimits
	// Logger receives the operational log (tool, code, duration, period,
	// count). Nil uses slog.Default.
	Logger *slog.Logger

	// The fields below are test seams; zero values are production values.

	// Now is the clock. Nil is time.Now.
	Now func() time.Time
	// QueryTimeout overrides the 3 s tool budget.
	QueryTimeout time.Duration
	// DriverName overrides the database/sql driver readdb.Open uses.
	DriverName string
	// InterfaceAddrs overrides net.InterfaceAddrs for the tailnet check.
	InterfaceAddrs settings.InterfaceAddrs
}

// Server is the MCP endpoint. Create it with New.
type Server struct {
	opts Options
	log  *slog.Logger
	now  func() time.Time
	// slots holds one token per running tool call; its capacity is
	// MaxConcurrent.
	slots chan struct{}

	mu sync.Mutex
	db *sql.DB // opened on the first authenticated tool call
	// writer is the narrow settings write connection, opened on the first
	// settings write (nothing else ever writes).
	writer *mcpset.Writer
}

// New returns a Server. It performs no I/O: nothing is bound and no database
// is opened until Listen / the first authenticated tool call.
func New(opts Options) *Server {
	s := &Server{opts: opts, slots: make(chan struct{}, MaxConcurrent)}
	if s.log = opts.Logger; s.log == nil {
		s.log = slog.Default()
	}
	if s.now = opts.Now; s.now == nil {
		s.now = time.Now
	}
	if s.opts.QueryTimeout <= 0 {
		s.opts.QueryTimeout = QueryTimeout
	}
	if s.opts.DriverName == "" {
		s.opts.DriverName = readdb.DriverName
	}
	return s
}

// checkListenable reports why the server must not bind, or nil. It logs the
// reason; no log line contains a secret.
func (s *Server) checkListenable() error {
	cfg := s.opts.Settings
	switch {
	case !cfg.Enabled:
		s.log.Info("paper-results-mcp: disabled; not listening")
		return ErrNotListening
	case cfg.APIKey == "":
		// Also when Cloudflare Access values are configured: they are not
		// an API key (§3.3).
		s.log.Error(logNoAuth)
		return ErrNotListening
	case cfg.VerifyAccessHeaders && (cfg.AccessClientID == "" || cfg.AccessClientSecret == ""):
		s.log.Error("paper-results-mcp: refusing to listen: access header verification enabled without id or secret")
		return ErrNotListening
	}
	if err := settings.ValidateListen(cfg.Listen, s.opts.InterfaceAddrs); err != nil {
		s.log.Error("paper-results-mcp: refusing to listen: listen address not allowed")
		return fmt.Errorf("%w: %w", ErrNotListening, err)
	}
	return nil
}

// Listen binds the configured address. When the settings forbid listening
// it returns ErrNotListening without binding. A port already in use is an
// error: there is no fallback address.
func (s *Server) Listen() (net.Listener, error) {
	if err := s.checkListenable(); err != nil {
		return nil, err
	}
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", s.opts.Settings.Listen)
	if err != nil {
		return nil, fmt.Errorf("paper-results-mcp: listen: %w", err)
	}
	return ln, nil
}

// Serve listens (see Listen) and serves until ctx is cancelled, then shuts
// down gracefully and closes the database. It returns ErrNotListening
// (wrapped) when the settings forbid listening.
func (s *Server) Serve(ctx context.Context) error {
	ln, err := s.Listen()
	if err != nil {
		return err
	}
	return s.ServeListener(ctx, ln)
}

// ServeListener serves on an already bound listener until ctx is cancelled.
func (s *Server) ServeListener(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
	done := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		done <- srv.Shutdown(shutdownCtx)
	}()
	s.log.Info("paper-results-mcp: listening", "addr", ln.Addr().String())
	err := srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		err = <-done
	}
	return errors.Join(err, s.Close())
}

// Close releases the database connection, if one was opened.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	if s.db != nil {
		errs = append(errs, s.db.Close())
		s.db = nil
	}
	if s.writer != nil {
		errs = append(errs, s.writer.Close())
		s.writer = nil
	}
	return errors.Join(errs...)
}

// DB returns the read-only handle, opening it on first use. A failed
// open is not cached, so a database that appears later is picked up.
func (s *Server) DB() (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		return s.db, nil
	}
	db, err := readdb.OpenWith(s.opts.DriverName, s.opts.DBPath)
	if err != nil {
		return nil, err
	}
	s.db = db
	return db, nil
}

// Writer returns the settings write connection, opening it on first use. A
// failed open is not cached. It is reached only from the settings tools that
// write, after the whitelist and the key check.
func (s *Server) Writer() (*mcpset.Writer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != nil {
		return s.writer, nil
	}
	w, err := mcpset.OpenWriter(s.opts.DriverName, s.opts.DBPath, s.opts.PaperLimits)
	if err != nil {
		return nil, err
	}
	w.SetClock(s.now)
	s.writer = w
	return w, nil
}

// ActiveCalls reports how many tool calls hold a concurrency slot (running
// or waiting for the connection). It is for diagnostics and tests.
func (s *Server) ActiveCalls() int { return len(s.slots) }
