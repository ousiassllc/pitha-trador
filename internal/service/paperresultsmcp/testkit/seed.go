package testkit

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Sentinel strings planted in every column the API must never expose. None
// may appear in any tool output or log line.
const (
	SentinelRaw       = "SENTINEL_RAW_DATA_JSON"
	SentinelState     = "SENTINEL_STATE_JSON"
	SentinelResponse  = "SENTINEL_RESPONSE_JSON"
	SentinelSecretKey = "SENTINEL_SECRET_KEY_NAME"
	SentinelSecret    = "SENTINEL_ENCRYPTED_VALUE"
	SentinelRuntime   = "SENTINEL_RUNTIME_SETTING"
	SentinelParams    = "SENTINEL_PARAMS_JSON"
	SentinelFeatures  = "SENTINEL_FEATURES_JSON"
	SentinelPayload   = "SENTINEL_PAYLOAD_JSON"
	SentinelLastError = "SENTINEL_LAST_ERROR"
	SentinelDetail    = "SENTINEL_DETAIL_JSON"
	SentinelBarError  = "SENTINEL_DAILY_BAR_ERROR"
	SentinelSkip      = "SENTINEL_SKIP_REASON"
	SentinelPriceCol  = 98765.4321 // a market_snapshots price no tool may return
)

func Sentinels() []string {
	return []string{SentinelRaw, SentinelState, SentinelResponse, SentinelSecretKey, SentinelSecret,
		SentinelRuntime, SentinelParams, SentinelFeatures, SentinelPayload, SentinelLastError,
		SentinelDetail, SentinelBarError, SentinelSkip, "98765.4321"}
}

// Stored renders an RFC3339 UTC instant the way the application stores it.
func Stored(t *testing.T, rfc3339 string) string {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		t.Fatal(err)
	}
	return sqlutil.FormatTime(ts)
}

var (
	templateOnce sync.Once
	templatePath string
	templateErr  error
)

// migratedTemplate builds one migrated database per test binary (the
// migrations are slow); tests copy it. sqlitedb.Open is used only here, by
// the test, to build the fixture - production code never calls it.
func migratedTemplate() (string, error) {
	templateOnce.Do(func() {
		dir, err := os.MkdirTemp("", "paperresultsmcp-template-")
		if err != nil {
			templateErr = err
			return
		}
		templatePath = filepath.Join(dir, "pitha.db")
		db, err := sqlitedb.Open(templatePath)
		if err != nil {
			templateErr = err
			return
		}
		templateErr = db.Close()
	})
	return templatePath, templateErr
}

// RemoveTemplate deletes the shared migrated template; call it from TestMain.
func RemoveTemplate() {
	if templatePath != "" {
		_ = os.RemoveAll(filepath.Dir(templatePath))
	}
}

// NewMigratedDB copies the migrated template into a temp dir and returns its
// path and a single-connection writer with foreign keys off so fixtures need
// not build every parent row.
func NewMigratedDB(t *testing.T) (string, *sql.DB) {
	t.Helper()
	tpl, err := migratedTemplate()
	if err != nil {
		t.Fatalf("build fixture db: %v", err)
	}
	raw, err := os.ReadFile(tpl)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pitha.db")
	if err := os.WriteFile(path, raw, 0o600); err != nil { //nolint:gosec // G703: path is under t.TempDir()
		t.Fatal(err)
	}
	w, err := sql.Open("sqlite", sqlitedb.FileURI(path, "_busy_timeout=5000"))
	if err != nil {
		t.Fatal(err)
	}
	w.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = w.Close() })
	if _, err := w.ExecContext(t.Context(), "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	return path, w
}

func MustExec(t *testing.T, w *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := w.ExecContext(t.Context(), query, args...); err != nil {
		t.Fatalf("seed %q: %v", FirstLine(query), err)
	}
}

func FirstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Position inserts a positions row. closed "" leaves closed_at NULL; pnl
// NaN-free nil leaves realized_pnl NULL.
func Position(t *testing.T, w *sql.DB, closed string, pnl *float64, entry float64, qty int, exitReason any, entryOrder, exitOrder any) {
	t.Helper()
	var closedAt any
	if closed != "" {
		closedAt = Stored(t, closed)
	}
	MustExec(t, w, `INSERT INTO positions (instrument_id, entry_order_id, exit_order_id, symbol, side, quantity,
		entry_price, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at, exit_reason)
		VALUES (1, ?, ?, '7203', 'LONG', ?, ?, 4242.5, 777.7, ?, ?, ?, ?)`,
		entryOrder, exitOrder, qty, entry, pnl, Stored(t, "2026-09-30T00:00:00Z"), closedAt, exitReason)
}

// SeedPositions plants the closed/open positions whose aggregates the tests
// compute by hand. Ids follow insertion order.
//
//	id closed_at (UTC)        JST day  pnl    entry*qty  note
//	1  2026-10-01T00:30:00Z   10-01    +1000  100000     exit order 1010, fees 10+20
//	2  2026-10-01T05:00:00Z   10-01    -3000  100000     entry order only, fees 15
//	3  2026-10-02T01:00:00Z   10-02    +2000  200000     no orders
//	4  2026-10-02T15:30:00Z   10-03    0      100000     flat
//	5  2026-10-03T02:00:00Z   10-03    -500   0          entry price 0
//	6  open                                              excluded from closed
//	7  2026-10-04T00:00:00Z   pnl NULL                    excluded
//	8  2026-08-01T00:00:00Z   +99999                      outside the period
func SeedPositions(t *testing.T, w *sql.DB) {
	t.Helper()
	MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'sentinel-free', 'TSE')`)
	order := func(id int, price any, fees float64) {
		MustExec(t, w, `INSERT INTO paper_orders (id, instrument_id, symbol, side, order_type, quantity, limit_price,
			status, submitted_at, filled_price, fees, slippage_bps) VALUES (?, 1, '7203', 'BUY', 'LIMIT', 100, 555.5,
			'FILLED', ?, ?, ?, 12.3)`, id, Stored(t, "2026-09-30T00:00:00Z"), price, fees)
	}
	order(1, 1000.0, 10)
	order(2, 1010.0, 20)
	order(3, 1000.0, 15)
	Position(t, w, "2026-10-01T00:30:00Z", new(float64(1000)), 1000, 100, "take_profit", 1, 2)
	Position(t, w, "2026-10-01T05:00:00Z", new(float64(-3000)), 1000, 100, "stop_loss", 3, nil)
	Position(t, w, "2026-10-02T01:00:00Z", new(float64(2000)), 2000, 100, nil, 99, nil)
	Position(t, w, "2026-10-02T15:30:00Z", new(float64(0)), 1000, 100, nil, 99, nil)
	Position(t, w, "2026-10-03T02:00:00Z", new(float64(-500)), 0, 100, nil, 99, nil)
	Position(t, w, "", nil, 1000, 100, nil, 99, nil)
	Position(t, w, "2026-10-04T00:00:00Z", nil, 1000, 100, nil, 99, nil)
	Position(t, w, "2026-08-01T00:00:00Z", new(float64(99999)), 1000, 100, nil, 99, nil)
}

// FixedNow is "now" in tests: 2026-10-18 12:00 JST. The default period is
// therefore [2026-09-19, 2026-10-19) JST.
func FixedNow() time.Time { return time.Date(2026, 10, 18, 3, 0, 0, 0, time.UTC) }
