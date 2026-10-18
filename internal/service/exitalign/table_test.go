package exitalign_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
)

const docsPath = "../../../docs/requirements/functional/components-platform.md"

// shippedExit is the actual exit the Paper Execution Engine ships with.
func shippedExit() exitalign.ExitConfig {
	cfg := execution.DefaultConfig()
	return exitalign.ExitConfig{
		StopLossPct: cfg.StopLossPct, TakeProfitPct: cfg.TakeProfitPct,
		TrailingStopPct: cfg.TrailingStopPct, MaxHoldingMinutes: cfg.MaxHoldingMinutes,
	}
}

var backticked = regexp.MustCompile("`([0-9.]+)`")

// docsTable returns, per row label of the FR-CAL-4A table, the numbers in
// backticks of its label cell and of its exit cell.
func docsTable(t *testing.T) map[string][2][]float64 {
	t.Helper()
	raw, err := os.ReadFile(docsPath)
	if err != nil {
		t.Fatalf("read %s: %v", docsPath, err)
	}
	text := string(raw)
	start := strings.Index(text, "<!-- label-exit-table:start -->")
	end := strings.Index(text, "<!-- label-exit-table:end -->")
	if start < 0 || end < start {
		t.Fatalf("%s has no label-exit-table markers", docsPath)
	}
	rows := map[string][2][]float64{}
	for _, line := range strings.Split(text[start:end], "\n") {
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) != 3 || strings.HasPrefix(strings.TrimSpace(cells[0]), "---") || strings.TrimSpace(cells[0]) == "項目" {
			continue
		}
		numbers := func(cell string) []float64 {
			var out []float64
			for _, m := range backticked.FindAllStringSubmatch(cell, -1) {
				v, err := strconv.ParseFloat(m[1], 64)
				if err != nil {
					t.Fatalf("number %q in %q: %v", m[1], cell, err)
				}
				out = append(out, v)
			}
			return out
		}
		rows[strings.TrimSpace(cells[0])] = [2][]float64{numbers(cells[1]), numbers(cells[2])}
	}
	return rows
}

func equalNumbers(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The FR-CAL-4A table and the code defaults must agree: changing a default in
// barriersettings, execution or costsettings without the table (or the table
// without the code) fails here.
func TestDocsTableMatchesCodeDefaults(t *testing.T) {
	b, exit, costs := barriersettings.Default(), shippedExit(), costsettings.Default()
	cost := costs.LabelCostBps(b.CostBps, domain.JevDirectionLong)
	fill := costs.RoundTripBps(domain.JevDirectionLong)
	want := map[string][2][]float64{
		"利確":         {{b.TakeProfitBps, cost, b.TakeProfitBps + cost}, {exit.TakeProfitPct, exit.TakeProfitPct * 100}},
		"損切り":        {{b.StopLossBps, cost, b.StopLossBps - cost}, {exit.StopLossPct, exit.StopLossPct * 100}},
		"保有時間":       {{float64(b.TimeoutMinutes())}, {float64(exit.MaxHoldingMinutes)}},
		"コスト（往復）":    {{cost}, {fill}},
		"トレーリングストップ": {nil, {exit.TrailingStopPct}},
	}
	got := docsTable(t)
	if len(got) != len(want) {
		t.Fatalf("docs table has %d rows, want %d: %v", len(got), len(want), got)
	}
	for item, w := range want {
		g, ok := got[item]
		if !ok {
			t.Errorf("docs table has no row %q", item)
			continue
		}
		if !equalNumbers(g[0], w[0]) || !equalNumbers(g[1], w[1]) {
			t.Errorf("docs row %q = label %v / exit %v, code defaults give label %v / exit %v", item, g[0], g[1], w[0], w[1])
		}
	}
}

func warningCodes(ws []exitalign.AlignmentWarning) map[string]bool {
	out := map[string]bool{}
	for _, w := range ws {
		out[w.Code] = true
	}
	return out
}

// At the shipped settings the label basis differs from the actual exit in
// take-profit, stop-loss and horizon (the documented, accepted state), while
// the label cost covers the fill model's cost.
func TestCorrespondence_DefaultsWarn(t *testing.T) {
	rows, warnings := exitalign.Correspondence(barriersettings.Default(), shippedExit(), costsettings.Default())
	codes := warningCodes(warnings)
	for _, code := range []string{exitalign.WarnTakeProfit, exitalign.WarnStopLoss, exitalign.WarnHorizon} {
		if !codes[code] {
			t.Errorf("no %s warning at the defaults; warnings = %v", code, warnings)
		}
	}
	if codes[exitalign.WarnCost] || codes[exitalign.WarnCostSides] || len(warnings) != 3 {
		t.Errorf("unexpected cost warnings at the defaults: %v", warnings)
	}
	if len(rows) != 5 {
		t.Fatalf("len(rows) = %d, want 5", len(rows))
	}
	for _, row := range rows {
		if row.Key == "cost" && !row.Aligned {
			t.Errorf("cost row not aligned at the defaults: %+v", row)
		}
		if row.Key == "trailing_stop" && (row.Comparable || row.Aligned) {
			t.Errorf("trailing row = %+v, want not comparable and not aligned while the trailing stop is on", row)
		}
	}
}

// Aligning the exit with the label (option (b)) silences the warnings, within
// the tolerance; moving one side out of it brings its warning back.
func TestCorrespondence_AlignedAndDrifted(t *testing.T) {
	b := barriersettings.Default() // gross TP 40 / SL 20 bps, 10 min
	aligned := exitalign.ExitConfig{TakeProfitPct: 0.40, StopLossPct: 0.20, MaxHoldingMinutes: 10}
	if _, warnings := exitalign.Correspondence(b, aligned, costsettings.Default()); len(warnings) != 0 {
		t.Fatalf("aligned config warns: %v", warnings)
	}

	nearEdge := exitalign.ExitConfig{TakeProfitPct: 0.45, StopLossPct: 0.25, MaxHoldingMinutes: 11} // exactly at the tolerances
	if _, warnings := exitalign.Correspondence(b, nearEdge, costsettings.Default()); len(warnings) != 0 {
		t.Fatalf("config within tolerance warns: %v", warnings)
	}

	for name, tc := range map[string]struct {
		exit exitalign.ExitConfig
		code string
	}{
		"tp":      {exitalign.ExitConfig{TakeProfitPct: 0.46, StopLossPct: 0.20, MaxHoldingMinutes: 10}, exitalign.WarnTakeProfit},
		"sl":      {exitalign.ExitConfig{TakeProfitPct: 0.40, StopLossPct: 0.26, MaxHoldingMinutes: 10}, exitalign.WarnStopLoss},
		"horizon": {exitalign.ExitConfig{TakeProfitPct: 0.40, StopLossPct: 0.20, MaxHoldingMinutes: 12}, exitalign.WarnHorizon},
		"tp off":  {exitalign.ExitConfig{StopLossPct: 0.20, MaxHoldingMinutes: 10}, exitalign.WarnTakeProfit},
	} {
		_, warnings := exitalign.Correspondence(b, tc.exit, costsettings.Default())
		if codes := warningCodes(warnings); !codes[tc.code] || len(warnings) != 1 {
			t.Errorf("%s: warnings = %v, want only %s", name, warnings, tc.code)
		}
	}
}

// A trailing stop that is off leaves its row aligned; the label cost below the
// fill model's round trip, or far above it, warns.
func TestCorrespondence_CostAndTrailing(t *testing.T) {
	b := barriersettings.Default()
	exit := exitalign.ExitConfig{TakeProfitPct: 0.40, StopLossPct: 0.20, MaxHoldingMinutes: 10}
	rows, _ := exitalign.Correspondence(b, exit, costsettings.Default())
	for _, row := range rows {
		if row.Key == "trailing_stop" && !row.Aligned {
			t.Errorf("trailing row with the stop off = %+v, want aligned", row)
		}
	}

	// The label cost follows the cost settings, so the base cost is what can
	// fall short of the fill model: base 2 < the 4 bps the defaults charge.
	low := b
	low.CostBps = 2
	_, warnings := exitalign.Correspondence(low, exit, costsettings.Default())
	if !warningCodes(warnings)[exitalign.WarnCost] {
		t.Errorf("label cost 2 bps below the 4 bps fill cost does not warn: %v", warnings)
	}
	high := b
	high.CostBps = 25 // allowance 21 bps > 15
	_, warnings = exitalign.Correspondence(high, exit, costsettings.Default())
	if !warningCodes(warnings)[exitalign.WarnCost] {
		t.Errorf("a 21 bps spread allowance does not warn: %v", warnings)
	}

	// SHORT borrow fees move only the SHORT label cost: the directions differ.
	costs := costsettings.Default()
	costs.ShortBorrowBps = 8
	_, warnings = exitalign.Correspondence(b, exit, costs)
	if !warningCodes(warnings)[exitalign.WarnCostSides] {
		t.Errorf("LONG/SHORT label cost gap of 8 bps does not warn: %v", warnings)
	}
}

// cost_mismatch reports the base cost (calibration.barrier_cost_bps) straying
// from the defaults, not the fee or slippage: the label cost moves with the
// fill model's round-trip cost, so changing either leaves the gap, and the
// warning, as it was.
func TestCostWarning_IgnoresFeeAndSlippageChanges(t *testing.T) {
	b := barriersettings.Default()
	exit := shippedExit()
	costs := costsettings.Default()
	costs.FeeBps = 10
	costs.Long.ContinuousBps = 12
	costs.Short.ContinuousBps = 12
	_, warnings := exitalign.Correspondence(b, exit, costs)
	if warningCodes(warnings)[exitalign.WarnCost] {
		t.Errorf("raising the fee and slippage on the default base cost warns: %v", warnings)
	}
}
