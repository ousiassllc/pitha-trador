package main

import (
	"context"
	"fmt"
	"io"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap/backtestsource"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// comparer is backtestsource.Source's side of -compare-legacy: the RunConfigs
// of period, replayed in Mode().
type comparer interface {
	RunConfigs(ctx context.Context, period backtest.Period) ([]backtest.RunConfig, error)
	Mode() backtestsource.ReplayMode
}

// compareFunc is backtest.CompareEntryRules (a seam for the tests only).
type compareFunc func(ctx context.Context, cfgs []backtest.RunConfig, period backtest.Period) (backtest.EntryComparison, error)

// compareLegacy replays period under the live entry rule and under the
// pre-#765 confidence-threshold rule (issue #1114, #775) and prints the two
// metrics and their difference. It stores nothing and judges nothing, so every
// outcome but an error is exitOK.
func compareLegacy(ctx context.Context, c comparer, compare compareFunc, period backtest.Period, stdout, stderr io.Writer) int {
	configs, err := c.RunConfigs(ctx, period)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "backtestverify:", err)
		return exitError
	}
	cmp, err := compare(ctx, configs, period)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "backtestverify:", err)
		return exitError
	}
	_, _ = fmt.Fprintf(stdout, "再生モード: %s（min_entry_quality: %s）\n", c.Mode(), modeDescription(c.Mode()))
	_, _ = fmt.Fprintf(stdout, "期間: %s 〜 %s (JST)\n", period.Start.In(marketcalendar.JST).Format("2006-01-02 15:04"), period.End.In(marketcalendar.JST).Format("2006-01-02 15:04"))
	_, _ = io.WriteString(stdout, formatComparison(cmp))
	return exitOK
}

// formatComparison is the operator-facing comparison (Japanese): the same
// numbers parity.Report prints for a replay, per entry rule, and Meta minus Legacy.
func formatComparison(c backtest.EntryComparison) string {
	return fmt.Sprintf("新旧エントリールール比較（表示のみ。取引・保存には使わない）\n"+
		"  新ルール(Meta・現行):          %s\n"+
		"  旧ルール(Legacy・確信度しきい値): %s\n"+
		"  差（新−旧）: 件数 %+d 勝率 %+.1fpt Expectancy %+.3f%% MaxDD %+.3f%% 純損益 %+.3f%%\n",
		formatMetrics(c.Meta), formatMetrics(c.Legacy),
		c.Meta.TradeCount-c.Legacy.TradeCount, (c.Meta.WinRate-c.Legacy.WinRate)*100, c.ExpectancyDiff(),
		c.Meta.MaxDrawdownPct-c.Legacy.MaxDrawdownPct, c.Meta.NetPnLPct-c.Legacy.NetPnLPct)
}

func formatMetrics(m backtest.Metrics) string {
	return fmt.Sprintf("件数 %d 勝率 %.1f%% Expectancy %.3f%% PF %.3f MaxDD %.3f%% 純損益 %.3f%%",
		m.TradeCount, m.WinRate*100, m.Expectancy, m.ProfitFactor, m.MaxDrawdownPct, m.NetPnLPct)
}
