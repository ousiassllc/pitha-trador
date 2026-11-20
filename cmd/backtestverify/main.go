// Command backtestverify is the operator's reproducibility check for the
// backtest (issue #775, FR-BT-4): it replays a period of the recorded
// market_snapshots/jev_decisions through the live decision flow, exactly as the
// shadow backtest does, compares the replayed entries and exit reasons with the
// Paper positions actually opened in it, and prints the numbers
// (parity.Report.String).
//
// The verdict is judged against parity.Provisional's lower bounds, which no
// flag overrides:
//
//	exit 0  合格, or データ無し (Paper made no entry in the period, so there is
//	        nothing to reproduce - the report says so; it is not a failure)
//	exit 1  不合格: a match rate is below its lower bound (reason on stderr)
//	exit 2  bad flags, or the database/config/replay could not be read
//
// -from and -to are JST dates (YYYY-MM-DD), both inclusive: the period is
// [-from 00:00, -to + 1 day 00:00) JST. With neither, the period is the shadow
// backtest's own window (selfimprove.ShadowBacktestPeriod: the last 30 TSE
// business days up to now, FR-SELFIMPROVE-4); giving only one is an error.
//
// -replay-mode picks the min_entry_quality the replay judges by (issue #874):
// live (default: policy.*.min_entry_quality, the Live value, as before) or paper
// (policy.paper.*.min_entry_quality, what the Paper Policy Engine decides with,
// so a candidate that passes under Paper's good passes in the replay). The
// report's first line names it.
//
// -compare-legacy (issue #1114) replaces the reproducibility check with the
// comparison of the live entry rule (meta-labeling) and the pre-#765
// confidence-threshold rule over the same period (backtest.CompareEntryRules):
// 件数・勝率・Expectancy・PF・MaxDD・純損益 of each and Meta minus Legacy. It
// honours -replay-mode and the period flags, is display only (nothing is
// stored or traded on) and exits 0, or 2 when the replay cannot be read.
// Without it nothing changes.
//
// It is read-only: the database is opened with SQLite mode=ro (no migration;
// SQLite may still create the empty -wal/-shm side files next to a WAL
// database, see sqlitedb.OpenReadOnly) and nothing is stored. -db defaults to
// PITHA_DB_PATH, then the per-user pitha.db the application uses;
// PITHA_STRATEGY_PATH/PITHA_RISK_PATH select the config like the application
// does (bootstrap.Config).
//
//	backtestverify -db pitha.db -from 2026-10-01 -to 2026-10-16
//	backtestverify -db pitha.db
//	backtestverify -db pitha.db -replay-mode paper
//	backtestverify -db pitha.db -from 2026-10-01 -to 2026-10-16 -compare-legacy
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backtestsource"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/parity"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// Exit codes (see the package comment).
const (
	exitOK     = 0
	exitFailed = 1
	exitError  = 2
)

const dayLayout = "2006-01-02"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, time.Now))
}

// verifier is backtestsource.Source's reproducibility check.
type verifier interface {
	VerifyReproducibility(ctx context.Context, period backtest.Period, th parity.Thresholds) (parity.Report, error)
	Mode() backtestsource.ReplayMode
}

// run parses args, opens the database read-only and verifies; it returns the
// process exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, now func() time.Time) int {
	opts, err := parseArgs(args, stderr, now())
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(stderr, "backtestverify:", err)
		}
		return exitError
	}
	state, err := bootstrap.Run(bootstrap.Config{DBPath: opts.dbPath, ReadOnly: true})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "backtestverify:", err)
		return exitError
	}
	defer func() { _ = state.Close() }()
	source := backtestsource.NewForDB(state.DB, state.Strategy, state.Risk, opts.mode)
	if opts.compareLegacy {
		return compareLegacy(ctx, source, backtest.CompareEntryRules, opts.period, stdout, stderr)
	}
	return verify(ctx, source, opts.period, stdout, stderr)
}

// verify runs the check over period against parity.Provisional and maps the
// report to the exit code: Report.Err (the 不合格 verdict) is exitFailed,
// every other verdict - 合格 and データ無し - is exitOK.
func verify(ctx context.Context, v verifier, period backtest.Period, stdout, stderr io.Writer) int {
	report, err := v.VerifyReproducibility(ctx, period, parity.Provisional)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "backtestverify:", err)
		return exitError
	}
	_, _ = fmt.Fprintf(stdout, "再生モード: %s（min_entry_quality: %s）\n", v.Mode(), modeDescription(v.Mode()))
	_, _ = fmt.Fprintf(stdout, "期間: %s 〜 %s (JST)\n", period.Start.In(marketcalendar.JST).Format("2006-01-02 15:04"), period.End.In(marketcalendar.JST).Format("2006-01-02 15:04"))
	_, _ = io.WriteString(stdout, report.String())
	if err := report.Err(); err != nil {
		_, _ = fmt.Fprintln(stderr, "backtestverify:", err)
		return exitFailed
	}
	return exitOK
}

// modeDescription names the min_entry_quality a replay mode judges by.
func modeDescription(mode backtestsource.ReplayMode) string {
	if mode == backtestsource.ReplayPaper {
		return "Paper用 policy.paper.*"
	}
	return "Live用 policy.*"
}

// options are the parsed flags.
type options struct {
	dbPath        string // "" lets bootstrap resolve it
	period        backtest.Period
	mode          backtestsource.ReplayMode
	compareLegacy bool
}

// parseArgs returns the parsed flags; now is the end of the default window.
func parseArgs(args []string, stderr io.Writer, now time.Time) (options, error) {
	fs := flag.NewFlagSet("backtestverify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	db := fs.String("db", "", "path to the pitha-trador SQLite database (opened read-only; default PITHA_DB_PATH, then the per-user pitha.db)")
	fromText := fs.String("from", "", "first day, JST, YYYY-MM-DD (default: with -to unset, the last 30 business days)")
	toText := fs.String("to", "", "last day (inclusive), JST, YYYY-MM-DD")
	modeText := fs.String("replay-mode", string(backtestsource.ReplayLive), "min_entry_quality the replay judges by: live (policy.*, default) or paper (policy.paper.*)")
	compare := fs.Bool("compare-legacy", false, "instead of the reproducibility check, print the live (meta-labeling) vs legacy (confidence-threshold) entry rule comparison over the period; display only, honours -replay-mode, exit 0 unless it fails")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	mode, err := backtestsource.ParseReplayMode(*modeText)
	if err != nil {
		return options{}, fmt.Errorf("-replay-mode: %w", err)
	}
	period, err := parsePeriod(*fromText, *toText, now)
	return options{dbPath: *db, period: period, mode: mode, compareLegacy: *compare}, err
}

// parsePeriod turns the inclusive JST day range into [from, to+1 day), or, with
// neither given, the shadow backtest's window ending at now.
func parsePeriod(fromText, toText string, now time.Time) (backtest.Period, error) {
	switch {
	case fromText == "" && toText == "":
		return selfimprove.ShadowBacktestPeriod(now), nil
	case fromText == "" || toText == "":
		return backtest.Period{}, errors.New("-from and -to must be given together (YYYY-MM-DD, JST), or neither for the last 30 business days")
	}
	from, err := time.ParseInLocation(dayLayout, fromText, marketcalendar.JST)
	if err != nil {
		return backtest.Period{}, fmt.Errorf("-from %q: %w", fromText, err)
	}
	last, err := time.ParseInLocation(dayLayout, toText, marketcalendar.JST)
	if err != nil {
		return backtest.Period{}, fmt.Errorf("-to %q: %w", toText, err)
	}
	if last.Before(from) {
		return backtest.Period{}, fmt.Errorf("-to %s is before -from %s", toText, fromText)
	}
	return backtest.Period{Start: from, End: last.AddDate(0, 0, 1)}, nil
}
