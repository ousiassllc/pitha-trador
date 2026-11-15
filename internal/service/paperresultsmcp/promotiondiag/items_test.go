package promotiondiag

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/promotionsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
)

func TestMaxDrawdown(t *testing.T) {
	for name, c := range map[string]struct {
		values []float64
		want   float64
	}{
		"empty":                    {nil, 0},
		"only gains":               {[]float64{5, 5, 5}, 0},
		"the start counts as peak": {[]float64{-10, 4}, 10},
		"peak to trough":           {[]float64{10, -4, -6, 3, -1}, 10}, // 10 -> 0, then 3 -> 2 is only 1
		"later deeper":             {[]float64{3, -1, 8, -9, 2}, 9},    // 10 -> 1 is 9; 3 -> 2 is 1
	} {
		if got := maxDrawdown(c.values); got != c.want {
			t.Errorf("%s: maxDrawdown = %v, want %v", name, got, c.want)
		}
	}
}

// rows builds common rows (time order) from day, candidate net, current net.
func rows(spec ...[3]any) []ledgerdiag.PairRow {
	out := make([]ledgerdiag.PairRow, len(spec))
	for i, s := range spec {
		out[i] = ledgerdiag.PairRow{DecisionID: int64(i + 1), Day: s[0].(string), Net: s[1].(float64), Ref: s[2].(float64)}
	}
	return out
}

func TestDayItemsFromHandSeries(t *testing.T) {
	// day diffs: 10-01: +9 (7+2), 10-02: -2, 10-03: +1  -> 2 of 3 days positive; total 8, without the best day -1
	e := &ledgerdiag.Entry{ID: "shadow:x", Series: rows(
		[3]any{"2026-10-01", 7.0, 0.0}, [3]any{"2026-10-01", 2.0, 0.0},
		[3]any{"2026-10-02", 0.0, 2.0},
		[3]any{"2026-10-03", 1.0, 0.0},
	)}
	n := 4
	e.NCommon = &n
	e.DiffVsLive = &ledgerdiag.Diff{N: 4, Days: 3}
	l := &ledgerdiag.Ledger{Reference: ledgerdiag.Reference{Rows: 4, ModelIDs: []string{"m"}}, LLMCutoff: evaldiag.Cutoff{Applied: true, Status: "confirmed"}}
	items, kn := evaluate(l, e, promotionsettings.Floor(), Request{})
	byName := map[string]Item{}
	for _, it := range items {
		byName[it.Name] = it
	}
	if p := byName[ItemPositiveDays]; p.Status != StatusPass || p.Actual.(float64) < 66.6 || p.Actual.(float64) > 66.7 || p.Required.(float64) != 50 {
		t.Errorf("positive_days = %+v", p)
	}
	if o := byName[ItemNotOneDayWins]; o.Status != StatusFail || o.Actual.(float64) != -1 || o.Detail != DetailOneDayCarries {
		t.Errorf("not_one_day_wins = %+v", o)
	}
	// candidate cum: 7, 9, 9, 10 -> drawdown 0; current: 0, 0, 2, 2 -> 0 ... both 0: not worse
	if d := byName[ItemMaxDrawdown]; d.Status != StatusPass || d.Actual.(float64) != 0 {
		t.Errorf("max_drawdown = %+v", d)
	}
	if kn.PositiveDayPct == nil || kn.CommonRows != 4 {
		t.Errorf("key numbers = %+v", kn)
	}
	if len(items) != 11 || items[0].Name != ItemSameRows || items[10].Name != ItemModelVersionStable {
		t.Errorf("order = %+v", items)
	}
}

func TestDrawdownWorseThanCurrentFails(t *testing.T) {
	// the candidate falls 6 then recovers; the current only falls 2
	e := &ledgerdiag.Entry{Series: rows([3]any{"d1", 3.0, 1.0}, [3]any{"d1", -6.0, -2.0}, [3]any{"d2", 9.0, 1.0})}
	it := drawdownItem(e)
	if it.Status != StatusFail || it.Actual.(float64) != 6 || it.Required.(float64) != 2 || it.Detail != DetailWorseDrawdown {
		t.Errorf("drawdown = %+v", it)
	}
	e.Series = rows([3]any{"d1", 3.0, 1.0}, [3]any{"d1", -2.0, -2.0})
	if it := drawdownItem(e); it.Status != StatusPass {
		t.Errorf("equal drawdown must pass: %+v", it)
	}
	if it := drawdownItem(&ledgerdiag.Entry{}); it.Status != StatusUnknown {
		t.Errorf("no rows must be unknown: %+v", it)
	}
}

func TestCompareUnknownIsNeverPassOrFail(t *testing.T) {
	if it := compare("x", ">", nil, 0, "why"); it.Status != StatusUnknown || it.Detail != "why" || it.Actual != nil {
		t.Errorf("nil = %+v", it)
	}
	v := 0.0
	if it := compare("x", ">", &v, 0, "why"); it.Status != StatusFail { // strict
		t.Errorf("0 > 0 = %+v", it)
	}
	if it := compare("x", ">=", &v, 0, "why"); it.Status != StatusPass {
		t.Errorf("0 >= 0 = %+v", it)
	}
	if it := compare("x", "<=", &v, 0, "why"); it.Status != StatusPass {
		t.Errorf("0 <= 0 = %+v", it)
	}
}

// The settings reader serves the five criteria only: any other key (a secret's path included) is refused
// before a statement is issued.
func TestSettingsReaderRefusesOtherKeys(t *testing.T) {
	r := settingsReader{q: noSQL{t}}
	for _, key := range []string{"llm.api_key", "jev.live_prompt_version", "research.eval.enabled", "calibration.barrier_cost_bps"} {
		if _, _, err := r.Get(context.Background(), key); err == nil || !strings.Contains(err.Error(), "not readable") {
			t.Errorf("%s: err = %v", key, err)
		}
	}
}

type noSQL struct{ t *testing.T }

func (n noSQL) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	n.t.Fatal("a statement was issued")
	return nil, nil
}

func (n noSQL) QueryRowContext(context.Context, string, ...any) *sql.Row {
	n.t.Fatal("a statement was issued")
	return nil
}
