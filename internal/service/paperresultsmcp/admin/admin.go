package admin

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// dialTimeout bounds the "is anything listening" probe so the Settings
// panel never hangs on it.
const dialTimeout = 300 * time.Millisecond

// Options configure a Service; every field but Path has a production default.
type Options struct {
	// Path is the settings file (settings.DefaultPath() in production).
	Path string
	// ExePath returns where paper-results-mcp is expected: next to the
	// running executable. Default: see DefaultExePath.
	ExePath func() (string, error)
	// Dial reports whether a TCP connection to address succeeds. Default: a
	// 300 ms net.Dialer connect, closed at once.
	Dial func(ctx context.Context, address string) error
	// InterfaceAddrs lists this host's addresses for settings.ValidateListen
	// (default net.InterfaceAddrs).
	InterfaceAddrs settings.InterfaceAddrs
}

// Service reads and edits the MCP's settings file. It holds no key between
// calls.
type Service struct {
	opts Options
	// mu serialises the read-modify-write cycles of Update and RegenerateKey.
	mu sync.Mutex
}

// New returns a Service over opts.
func New(opts Options) *Service {
	if opts.ExePath == nil {
		opts.ExePath = DefaultExePath
	}
	if opts.Dial == nil {
		opts.Dial = dialTCP
	}
	return &Service{opts: opts}
}

// ExeName is the MCP process's file name on this platform.
func ExeName() string {
	if runtime.GOOS == "windows" {
		return "paper-results-mcp.exe"
	}
	return "paper-results-mcp"
}

// DefaultExePath is the directory of the running executable plus ExeName:
// where the installer puts paper-results-mcp, beside the application.
func DefaultExePath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("paper-results-mcp: locate running executable: %w", err)
	}
	return filepath.Join(filepath.Dir(self), ExeName()), nil
}

func dialTCP(ctx context.Context, address string) error {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	return conn.Close()
}

// Update stores the switch and listen address, keeping the API key and the
// Access options of the existing file. An empty listen means
// settings.DefaultListen. The address must pass settings.ValidateListen (the
// MCP process applies the same rule at start): a rejected address returns an
// error wrapping settings.ErrListenNotAllowed and leaves the file untouched.
// A file that cannot be loaded is replaced by one made from the defaults and
// these values (its key is lost; View reports LoadError beforehand).
func (s *Service) Update(ctx context.Context, enabled bool, listen string) error {
	listen = strings.TrimSpace(listen)
	if listen == "" {
		listen = settings.DefaultListen
	}
	if err := settings.ValidateListen(listen, s.opts.InterfaceAddrs); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	cfg := s.current()
	cfg.Enabled, cfg.Listen = enabled, listen
	if err := settings.Save(s.opts.Path, cfg); err != nil {
		return err
	}
	slog.InfoContext(ctx, "paper-results-mcp: settings saved", "enabled", cfg.Enabled, "listen", cfg.Listen, "key_configured", cfg.APIKey != "")
	return nil
}

// RegenerateKey replaces the API key with a new random one, keeping the other
// fields, and returns it. The plaintext exists only in the return value:
// nothing logs or retains it, and View never shows it. The previous key stops
// working once the MCP process is restarted.
func (s *Service) RegenerateKey(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	key, err := settings.GenerateAPIKey()
	if err != nil {
		return "", err
	}
	cfg := s.current()
	cfg.APIKey = key
	if err := settings.Save(s.opts.Path, cfg); err != nil {
		return "", err
	}
	slog.InfoContext(ctx, "paper-results-mcp: API key regenerated", "enabled", cfg.Enabled, "listen", cfg.Listen, "key_configured", true)
	return key, nil
}

// current loads the stored settings, or the defaults when the file is absent
// or unusable (settings.Load returns Default alongside its error).
func (s *Service) current() settings.Settings {
	cfg, err := settings.Load(s.opts.Path)
	if err != nil {
		slog.Warn("paper-results-mcp: settings file unusable; rewriting it from defaults", "kind", loadErrorKind(err))
	}
	return cfg
}
