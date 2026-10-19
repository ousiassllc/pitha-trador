package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/parity"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

var jst = marketcalendar.JST

// fakeVerifier returns a canned report and records what it was asked.
type fakeVerifier struct {
	report parity.Report
	err    error

	gotPeriod     backtest.Period
	gotThresholds parity.Thresholds
}

func (f *fakeVerifier) VerifyReproducibility(_ context.Context, period backtest.Period, th parity.Thresholds) (parity.Report, error) {
	f.gotPeriod, f.gotThresholds = period, th
	return f.report, f.err
}

func fixtureTrade(symbol string, reason string) backtest.Trade {
	at := time.Date(2026, 10, 14, 9, 5, 0, 0, jst)
	return backtest.Trade{
		Symbol: symbol, Direction: domain.JevDirectionLong, EntryTimestamp: at,
		ExitTimestamp: at.Add(5 * time.Minute), ExitReason: reason, NetReturnPct: 0.2,
	}
}

func TestVerify_ExitCodeFollowsTheVerdict(t *testing.T) {
	passTrades := []backtest.Trade{fixtureTrade("7203", domain.ExitReasonTakeProfit)}
	tests := []struct {
		name       string
		report     parity.Report
		err        error
		wantCode   int
		wantStdout string
		wantStderr string
	}{
		{
			name:       "pass is exit 0",
			report:     parity.Compare(passTrades, passTrades, parity.Provisional),
			wantCode:   exitOK,
			wantStdout: "再現性検証: 合格",
		},
		{
			name:       "fail is exit 1 with the reason on stderr",
			report:     parity.Compare(passTrades, nil, parity.Provisional),
			wantCode:   exitFailed,
			wantStdout: "再現性検証: 不合格",
			wantStderr: "entry match rate 0.0% (lower bound 50.0%)",
		},
		{
			name:       "no Paper trades is exit 0 and says so",
			report:     parity.Compare(nil, passTrades, parity.Provisional),
			wantCode:   exitOK,
			wantStdout: "再現性検証: データ無し",
		},
		{
			name:       "a replay error is exit 2",
			err:        errors.New("read snapshots"),
			wantCode:   exitError,
			wantStderr: "read snapshots",
		},
	}
	period := backtest.Period{Start: time.Date(2026, 10, 14, 0, 0, 0, 0, jst), End: time.Date(2026, 10, 15, 0, 0, 0, 0, jst)}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := &fakeVerifier{report: tc.report, err: tc.err}
			var stdout, stderr bytes.Buffer
			if got := verify(context.Background(), v, period, &stdout, &stderr); got != tc.wantCode {
				t.Errorf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got, tc.wantCode, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.wantStdout) {
				t.Errorf("stdout missing %q:\n%s", tc.wantStdout, stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.wantStderr) {
				t.Errorf("stderr missing %q:\n%s", tc.wantStderr, stderr.String())
			}
			if v.gotThresholds != parity.Provisional || v.gotPeriod != period {
				t.Errorf("verifier got %+v over %+v, want parity.Provisional over the requested period", v.gotThresholds, v.gotPeriod)
			}
		})
	}
}

func TestParsePeriod(t *testing.T) {
	// Friday 2026-10-16 15:00 JST. Counting back 30 TSE business days skips
	// 10/12 (スポーツの日) and 9/21-9/23 (敬老の日・国民の休日・秋分の日), landing on 8/31 (JST midnight: the first day is whole).
	now := time.Date(2026, 10, 16, 15, 0, 0, 0, jst)

	got, err := parsePeriod("2026-10-01", "2026-10-16", now)
	if err != nil {
		t.Fatalf("parsePeriod: %v", err)
	}
	want := backtest.Period{Start: time.Date(2026, 10, 1, 0, 0, 0, 0, jst), End: time.Date(2026, 10, 17, 0, 0, 0, 0, jst)}
	if !got.Start.Equal(want.Start) || !got.End.Equal(want.End) {
		t.Errorf("explicit period = %v, want %v (-to inclusive)", got, want)
	}

	got, err = parsePeriod("", "", now)
	if err != nil {
		t.Fatalf("parsePeriod default: %v", err)
	}
	want = backtest.Period{Start: time.Date(2026, 8, 31, 0, 0, 0, 0, jst), End: now}
	if !got.Start.Equal(want.Start) || !got.End.Equal(want.End) {
		t.Errorf("default period = %v, want the last 30 business days %v", got, want)
	}

	for name, args := range map[string][2]string{
		"only -from":     {"2026-10-01", ""},
		"only -to":       {"", "2026-10-16"},
		"bad -from":      {"10/01", "2026-10-16"},
		"bad -to":        {"2026-10-01", "tomorrow"},
		"to before from": {"2026-10-16", "2026-10-01"},
	} {
		if _, err := parsePeriod(args[0], args[1], now); err == nil {
			t.Errorf("%s: parsePeriod(%q, %q) succeeded, want an error", name, args[0], args[1])
		}
	}
}

// seedDB creates a migrated database; with entryAt non-zero it records one
// closed Paper position opened then (and no market history to replay it from).
func seedDB(t *testing.T, entryAt time.Time) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pitha.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	defer func() { _ = db.Close() }()
	if entryAt.IsZero() {
		return path
	}
	ctx := context.Background()
	res, err := db.ExecContext(ctx, `INSERT INTO instruments (symbol, name, market, is_active) VALUES ('7203', 'トヨタ', 'TSE Prime', 1)`)
	if err != nil {
		t.Fatalf("insert instrument: %v", err)
	}
	instID, _ := res.LastInsertId()
	orders, positions := trading.NewOrderRepository(db), trading.NewPositionRepository(db)
	entry, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instID, Symbol: "7203", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
		Quantity: 100, Status: domain.OrderStatusPending, SubmittedAt: entryAt,
	})
	if err != nil {
		t.Fatalf("insert entry order: %v", err)
	}
	if entry, err = orders.Fill(ctx, entry.ID, 2100, 0, nil, entryAt); err != nil {
		t.Fatalf("fill entry order: %v", err)
	}
	pos, err := positions.Open(ctx, domain.Position{
		InstrumentID: instID, EntryOrderID: entry.ID, Symbol: "7203", Side: domain.PositionSideLong,
		Quantity: 100, EntryPrice: 2100, CurrentPrice: 2100, OpenedAt: entryAt,
	})
	if err != nil {
		t.Fatalf("open position: %v", err)
	}
	closeAt := entryAt.Add(30 * time.Minute)
	exit, err := orders.Insert(ctx, domain.PaperOrder{
		InstrumentID: instID, Symbol: "7203", Side: domain.OrderSideSell, OrderType: domain.OrderTypeMarket,
		Quantity: 100, Status: domain.OrderStatusFilled, SubmittedAt: closeAt,
	})
	if err != nil {
		t.Fatalf("insert exit order: %v", err)
	}
	if _, err := positions.Close(ctx, pos.ID, exit.ID, 2040, -6000, domain.ExitReasonStopLoss, closeAt); err != nil {
		t.Fatalf("close position: %v", err)
	}
	return path
}

func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	// The embedded default config, whatever the developer's environment says.
	t.Setenv(bootstrap.EnvStrategyPath, "")
	t.Setenv(bootstrap.EnvRiskPath, "")
	var out, errOut bytes.Buffer
	now := func() time.Time { return time.Date(2026, 10, 16, 15, 0, 0, 0, jst) }
	code = run(context.Background(), args, &out, &errOut, now)
	return code, out.String(), errOut.String()
}

func TestRun_EmptyDatabaseIsNoDataExitZero(t *testing.T) {
	db := seedDB(t, time.Time{})
	for name, args := range map[string][]string{
		"default window": {"-db", db},
		"explicit range": {"-db", db, "-from", "2026-10-01", "-to", "2026-10-16"},
	} {
		code, stdout, stderr := runCLI(t, args...)
		if code != exitOK {
			t.Errorf("%s: exit code = %d, want 0\nstdout:\n%s\nstderr:\n%s", name, code, stdout, stderr)
		}
		if !strings.Contains(stdout, "再現性検証: データ無し") || !strings.Contains(stdout, "Paperのエントリーが無い") {
			t.Errorf("%s: stdout does not say there was no Paper trade to reproduce:\n%s", name, stdout)
		}
	}
}

func TestRun_PaperEntryTheReplayCannotReproduceFails(t *testing.T) {
	db := seedDB(t, time.Date(2026, 10, 14, 9, 5, 0, 0, jst))

	code, stdout, stderr := runCLI(t, "-db", db, "-from", "2026-10-14", "-to", "2026-10-14")
	if code != exitFailed {
		t.Fatalf("exit code = %d, want 1\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "再現性検証: 不合格") || !strings.Contains(stdout, "Paper 1件 / 再生 0件 / 一致 0件") {
		t.Errorf("stdout = %s, want the failing report with Paper 1 / replay 0", stdout)
	}
	if !strings.Contains(stderr, "does not reproduce Paper trading") {
		t.Errorf("stderr = %s, want the report's own error", stderr)
	}

	// The same database, a period before the entry, has nothing to reproduce.
	if code, stdout, _ := runCLI(t, "-db", db, "-from", "2026-10-01", "-to", "2026-10-13"); code != exitOK || !strings.Contains(stdout, "データ無し") {
		t.Errorf("period without the entry: exit %d, stdout:\n%s; want exit 0 データ無し", code, stdout)
	}
}

func TestRun_DoesNotModifyTheDatabase(t *testing.T) {
	db := seedDB(t, time.Date(2026, 10, 14, 9, 5, 0, 0, jst))
	read := func() []byte {
		b, err := os.ReadFile(db)
		if err != nil {
			t.Fatalf("read database file: %v", err)
		}
		return b
	}
	before := read()
	if code, _, stderr := runCLI(t, "-db", db, "-from", "2026-10-14", "-to", "2026-10-14"); code != exitFailed {
		t.Fatalf("exit code = %d, want 1 (the run must have read the seeded position)\nstderr:\n%s", code, stderr)
	}
	if !bytes.Equal(read(), before) {
		t.Error("the database file changed: the check must be read-only")
	}
}

func TestRun_UsageAndOpenErrorsAreExitTwo(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.db")
	for name, args := range map[string][]string{
		"only -from":       {"-db", missing, "-from", "2026-10-01"},
		"unknown flag":     {"-bogus"},
		"stray argument":   {"-db", missing, "extra"},
		"missing database": {"-db", missing},
	} {
		code, stdout, stderr := runCLI(t, args...)
		if code != exitError || stdout != "" || stderr == "" {
			t.Errorf("%s: exit %d, stdout %q, stderr %q; want exit 2 with only stderr", name, code, stdout, stderr)
		}
	}
}
