// Command dailyguardreplay replays the FR-RISK-8 daily entry-halt rules over
// the positions recorded in a pitha-trador database (the positions table has
// no mode column, so point -db at a Paper database) and prints, per JST
// trading day, the result with and without the rules (issue #782).
//
// Every position opened or closed in the range is read: a day's trade count
// is by open day and its realized P&L by close day, so a position opened
// before -from still adds its P&L to the day it closed in, and one opened in
// the range but closed after -to (or still open) counts as an opened trade
// without P&L. Only the days of the range are printed. See
// dailyguardreplay.Limitations for what the replay cannot reproduce.
//
// It is read-only: the database is opened with SQLite mode=ro, no migration
// runs and the database file is not modified (SQLite may still create the
// empty -wal/-shm side files next to a WAL database; see
// sqlitedb.OpenReadOnly). Only the recorded realized P&L is replayed;
// the unrealized P&L the engine adds is not recoverable (see
// internal/service/dailyguardreplay). Thresholds come from the flags or, when
// none is given, from the database's runtime_settings.
//
//	dailyguardreplay -db pitha.db -from 2026-10-01 -to 2026-10-16
//	dailyguardreplay -db pitha.db -from 2026-10-01 -to 2026-10-16 \
//	    -capital 500000 -loss-pct 1 -trigger-pct 0.5 -giveback-pct 40 -max-trades 5
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/dailyguardreplay"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprintln(os.Stderr, "dailyguardreplay:", err)
		}
		os.Exit(2)
	}
}

// thresholdFlags maps each threshold flag to the runtime_settings key it
// sets, so values go through dailyguardsettings.Apply's range checks.
var thresholdFlags = []struct{ flag, key, usage string }{
	{"loss-jpy", dailyguardsettings.KeyLossLimitJPY, "daily loss limit in yen (direct)"},
	{"trigger-jpy", dailyguardsettings.KeyProfitLockTriggerJPY, "profit lock trigger in yen (direct)"},
	{"giveback-pct", dailyguardsettings.KeyProfitLockGivebackPct, "profit lock give-back percentage (0-100)"},
	{"max-trades", dailyguardsettings.KeyMaxTrades, "maximum trades opened per day"},
	{"capital", dailyguardsettings.KeyOperatingCapitalJPY, "operating capital in yen (derives -loss-pct / -trigger-pct)"},
	{"loss-pct", dailyguardsettings.KeyLossLimitPct, "daily loss limit as a percentage of -capital"},
	{"trigger-pct", dailyguardsettings.KeyProfitLockTriggerPct, "profit lock trigger as a percentage of -capital"},
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("dailyguardreplay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "path to the pitha-trador SQLite database (opened read-only)")
	fromText := fs.String("from", "", "first trading day, JST, YYYY-MM-DD")
	toText := fs.String("to", "", "last trading day (inclusive), JST, YYYY-MM-DD")
	values := make([]*string, len(thresholdFlags))
	for i, t := range thresholdFlags {
		values[i] = fs.String(t.flag, "", t.usage+"; with no threshold flag the database's runtime_settings are used")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbPath == "" {
		return errors.New("-db is required")
	}
	from, to, err := parseRange(*fromText, *toText)
	if err != nil {
		return err
	}

	db, err := sqlitedb.OpenReadOnly(*dbPath)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	limits, err := thresholds(ctx, values, system.NewRuntimeSettingsRepository(db))
	if err != nil {
		return err
	}
	positions, err := trading.NewPositionRepository(db).ListTouchingBetween(ctx, from, to)
	if err != nil {
		return err
	}
	trades := make([]dailyguardreplay.Trade, 0, len(positions))
	for _, p := range positions {
		t := dailyguardreplay.Trade{ID: p.ID, Symbol: p.Symbol, OpenedAt: p.OpenedAt}
		if p.ClosedAt != nil {
			t.ClosedAt = *p.ClosedAt
			t.PnLMissing = p.RealizedPnL == nil
		}
		if p.RealizedPnL != nil {
			t.PnLJPY = *p.RealizedPnL
		}
		trades = append(trades, t)
	}
	// Replay reports every day a position touched (the open day before -from,
	// the close day after -to); print only the requested days.
	return dailyguardreplay.Render(stdout, dailyguardreplay.Replay(trades, limits).Restrict(from, to))
}

// parseRange turns the inclusive JST day range into [from, to+1 day).
func parseRange(fromText, toText string) (from, to time.Time, err error) {
	if fromText == "" || toText == "" {
		return from, to, errors.New("-from and -to are required (YYYY-MM-DD, JST)")
	}
	if from, err = time.ParseInLocation(dailyguardreplay.DayLayout, fromText, dailyguardreplay.TradingDayZone); err != nil {
		return from, to, fmt.Errorf("-from %q: %w", fromText, err)
	}
	last, err := time.ParseInLocation(dailyguardreplay.DayLayout, toText, dailyguardreplay.TradingDayZone)
	if err != nil {
		return from, to, fmt.Errorf("-to %q: %w", toText, err)
	}
	if last.Before(from) {
		return from, to, fmt.Errorf("-to %s is before -from %s", toText, fromText)
	}
	return from, last.AddDate(0, 0, 1), nil
}

// thresholds resolves the limits from the threshold flags that were set, or,
// when none was, from the stored runtime_settings (what the risk engine
// reads).
func thresholds(ctx context.Context, values []*string, store dailyguardsettings.Store) (dailyguardsettings.Limits, error) {
	var s dailyguardsettings.Settings
	given := false
	for i, t := range thresholdFlags {
		if *values[i] == "" {
			continue
		}
		given = true
		if _, err := strconv.ParseFloat(*values[i], 64); err != nil {
			return dailyguardsettings.Limits{}, fmt.Errorf("-%s %q: not a number", t.flag, *values[i])
		}
		if err := dailyguardsettings.Apply(&s, t.key, *values[i]); err != nil {
			return dailyguardsettings.Limits{}, fmt.Errorf("-%s: %w", t.flag, err)
		}
	}
	if given {
		return dailyguardsettings.Resolve(s).Limits, nil
	}
	return dailyguardsettings.Load(ctx, store)
}
