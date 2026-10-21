package trainingflow_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
)

func fptr(v float64) *float64 { return &v }

func decidedRow(at time.Time, direction, state string) repo.TrainingRow {
	return repo.TrainingRow{
		JevDecisionID: 1, Symbol: "7203", DecidedAt: at, Direction: direction, HorizonMinutes: 15, NetReturnBps: 12,
		OrderFlow: domain.OrderFlowFeatures{OFIShort: fptr(10), OFIMid: fptr(20), OFILong: fptr(30), MicropriceDevBps: fptr(2)},
		StateJSON: state,
		ResponseJSON: `{"direction":"` + direction +
			`","entry_quality":"good","toxic_flow":0.1,"liquidity_stressed":0.1,"continuation_probability":0.6,"confidence":0.7}`,
	}
}

func TestBuildRows_RebuildsTheFeaturesTheDecisionHad(t *testing.T) {
	at := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	news := fmt.Sprintf(`{"symbol":"7203","items":[{"headline":"a","published_at":%q,"sentiment":"bullish","event_type":"other"}]}`, at.Add(-time.Hour).Format(time.RFC3339))
	state := `{"spread_bps":4.5,"return_1m":0.002,"news_context":` + news + `}`
	rows, dropped := training.BuildRows([]repo.TrainingRow{decidedRow(at, "SHORT", state)})
	if dropped != 0 || len(rows) != 1 {
		t.Fatalf("rows = %v, dropped %d", rows, dropped)
	}
	r := rows[0]
	if !r.DecidedAt.Equal(at) || !r.LabelEnd.Equal(at.Add(15*time.Minute)) || r.Direction != "SHORT" || r.NetReturnBps != 12 {
		t.Errorf("row = %+v", r)
	}
	if len(r.Vector) != len(metalabel.FeatureNames()) {
		t.Fatalf("vector = %v", r.Vector)
	}
	// SHORT: the direction-dependent features are negated (OFI x3, microprice, return).
	if r.Vector[0] != -10 || r.Vector[1] != -20 || r.Vector[2] != -30 || r.Vector[3] != -2 || r.Vector[4] != 4.5 || r.Vector[5] != -0.002 {
		t.Errorf("vector = %v, want the SHORT-oriented features", r.Vector)
	}
	if want := newsfeed.ScoreNewsContext([]domain.NewsContextItem{{PublishedAt: at.Add(-time.Hour), Sentiment: "bullish"}}, at, newsfeed.DefaultTTL); r.Vector[7] != want || want <= 0 {
		t.Errorf("tdnet score = %v, want %v (a fresh bullish disclosure as of the decision time)", r.Vector[7], want)
	}
	if r.Vector[6] <= 0 || r.Vector[6] > 1 {
		t.Errorf("jev score = %v, want a value in (0, 1]", r.Vector[6])
	}
}

func TestBuildRows_NoNewsContextIsNeutralAndIncompleteDecisionsAreDropped(t *testing.T) {
	at := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	noNews := decidedRow(at, "LONG", `{"spread_bps":4,"return_1m":0.001}`)
	missingSpread := decidedRow(at, "LONG", `{"return_1m":0.001}`)
	badState := decidedRow(at, "LONG", `not json`)
	badAnswer := decidedRow(at, "LONG", `{"spread_bps":4,"return_1m":0.001}`)
	badAnswer.ResponseJSON = `{"direction":"LONG"}`
	noFlow := decidedRow(at, "LONG", `{"spread_bps":4,"return_1m":0.001}`)
	noFlow.OrderFlow = domain.OrderFlowFeatures{}

	rows, dropped := training.BuildRows([]repo.TrainingRow{noNews, missingSpread, badState, badAnswer, noFlow})
	if len(rows) != 1 || dropped != 4 {
		t.Fatalf("rows %d, dropped %d, want 1 kept and 4 dropped (never a made-up feature)", len(rows), dropped)
	}
	if got := rows[0].Vector[7]; got != newsfeed.TDnetScoreNeutral {
		t.Errorf("tdnet score without news_context = %v, want neutral 0", got)
	}
}

// BuildRowsReport names, per decision, exactly the features BuildRows cannot
// complete and returns BuildRows' own rows and dropped count: a decision with
// no missing feature is kept, any missing feature drops it (the rule
// metalabel_training_readiness explains dropped_incomplete with).
func TestBuildRowsReport_AgreesWithBuildRows(t *testing.T) {
	at := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	ok := decidedRow(at, "LONG", `{"spread_bps":4,"return_1m":0.001}`)
	noSpread := decidedRow(at, "LONG", `{"return_1m":0.001}`)
	badState := decidedRow(at, "LONG", `not json`)
	badAnswer := decidedRow(at, "LONG", `{"spread_bps":4,"return_1m":0.001}`)
	badAnswer.ResponseJSON = `{"direction":"LONG"}`
	noFlow := decidedRow(at, "SHORT", `{"spread_bps":4,"return_1m":0.001}`)
	noFlow.OrderFlow = domain.OrderFlowFeatures{}

	for name, tc := range map[string]struct {
		row  repo.TrainingRow
		want map[string]int
	}{
		"complete":   {ok, map[string]int{}},
		"no spread":  {noSpread, map[string]int{"spread_bps": 1}},
		"bad state":  {badState, map[string]int{"spread_bps": 1, "return_1m": 1}},
		"bad answer": {badAnswer, map[string]int{"jev_score": 1}},
		"no flow":    {noFlow, map[string]int{"ofi_short": 1, "ofi_mid": 1, "ofi_long": 1, "microprice_dev_bps": 1}},
	} {
		rows, dropped, missing := training.BuildRowsReport([]repo.TrainingRow{tc.row})
		if fmt.Sprint(missing) != fmt.Sprint(tc.want) {
			t.Errorf("%s: missing = %v, want %v", name, missing, tc.want)
		}
		wantRows, wantDropped := training.BuildRows([]repo.TrainingRow{tc.row})
		if len(rows) != len(wantRows) || dropped != wantDropped {
			t.Errorf("%s: BuildRowsReport kept %d dropped %d, BuildRows kept %d dropped %d", name, len(rows), dropped, len(wantRows), wantDropped)
		}
		if kept := len(tc.want) == 0; kept != (len(rows) == 1 && dropped == 0) {
			t.Errorf("%s: missing %v but kept %d dropped %d", name, missing, len(rows), dropped)
		}
	}
}
