package loader_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/loader"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// TestDefaultEmbargoIsTheTrainingEmbargo pins the copied constant to the
// training package's walk-forward default (the loader must not import it).
func TestDefaultEmbargoIsTheTrainingEmbargo(t *testing.T) {
	if got, want := loader.DefaultEmbargo, training.DefaultWalkForwardConfig().Embargo; got != want {
		t.Errorf("DefaultEmbargo %v, training default %v", got, want)
	}
}

// TestBuildFeaturesMatchesTheTrainingRows: the features of a stored decision are
// rebuilt by the rule training.BuildRows uses, for a complete decision and for
// ones lacking a feature (never a made-up value).
func TestBuildFeaturesMatchesTheTrainingRows(t *testing.T) {
	at := time.Date(2026, 10, 29, 9, 10, 0, 0, time.UTC)
	f := func(v float64) *float64 { return &v }
	for name, d := range map[string]domain.JevDecision{
		"complete": {
			ID: 1, Timestamp: at, StateJSON: `{"spread_bps":3,"return_1m":0.001}`,
			ResponseJSON: `{"direction":"LONG","entry_quality":"good","toxic_flow":0.1,"liquidity_stressed":0.1,"continuation_probability":0.9,"confidence":0.8}`,
			OrderFlow:    domain.OrderFlowFeatures{OFIShort: f(2), OFIMid: f(2), OFILong: f(2), MicropriceDevBps: f(2)},
		},
		"unreadable state and answer": {ID: 2, Timestamp: at, StateJSON: `{`, ResponseJSON: `x`, OrderFlow: domain.OrderFlowFeatures{OFIShort: f(1), OFIMid: f(1), OFILong: f(1), MicropriceDevBps: f(1)}},
		"no order flow":               {ID: 3, Timestamp: at, StateJSON: `{"spread_bps":3,"return_1m":0.001}`, ResponseJSON: `{"direction":"LONG","entry_quality":"good","toxic_flow":0.1,"liquidity_stressed":0.1,"continuation_probability":0.9,"confidence":0.8}`},
	} {
		got := loader.BuildFeatures(d)
		rows, dropped, _ := training.BuildRowsReport([]repo.TrainingRow{{
			JevDecisionID: d.ID, DecidedAt: d.Timestamp, Direction: domain.JevDirectionLong, OrderFlow: d.OrderFlow,
			StateJSON: d.StateJSON, ResponseJSON: d.ResponseJSON, HorizonMinutes: 10, NetReturnBps: 1,
		}})
		complete := len(got.MissingNames()) == 0
		if complete != (dropped == 0) {
			t.Errorf("%s: complete=%v but training dropped=%d", name, complete, dropped)
		}
		if complete && !reflect.DeepEqual(got, rows[0].Features) {
			t.Errorf("%s: features differ from training's: %+v vs %+v", name, got, rows[0].Features)
		}
	}
}

// TestCohortOfAStoredDecision: the cohort is the one of its question version; an
// unknown version has none (so it matches no manifest cohort).
func TestCohortOfAStoredDecision(t *testing.T) {
	if got := loader.Cohort(domain.JevDecision{QuestionVersion: "trader-v3"}); got != "entry-quality-v3" {
		t.Errorf("trader-v3 cohort %q", got)
	}
	if got := loader.Cohort(domain.JevDecision{QuestionVersion: "nope"}); got != "" {
		t.Errorf("unknown version cohort %q", got)
	}
}

// TestLoadDecisionsKeepsEveryTraderDecisionAndMarksMissingBars.
func TestLoadDecisionsKeepsEveryTraderDecisionAndMarksMissingBars(t *testing.T) {
	db := runnertest.Build(t, runnertest.Options{MissingBarDay: 11})
	src, err := loader.Open(db.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = src.Close() }()
	dec, err := src.LoadDecisions(t.Context(), runnertest.Day(0), runnertest.Day(20), runnertest.Day(8))
	if err != nil {
		t.Fatal(err)
	}
	// 4 days x 4 training/OOS rows + 4 eval days x 8 + the other-cohort one.
	if got, want := len(dec.Rows), 16+32+1; got != want || len(dec.All) != want {
		t.Fatalf("rows %d / decisions %d, want %d", len(dec.Rows), len(dec.All), want)
	}
	missing, labeled := 0, 0
	for _, r := range dec.Rows {
		if r.Eligibility.Unrecoverable == loader.ReasonMarketDataUnavailable {
			missing++
		}
		if r.Eligibility.Labeled {
			labeled++
		}
	}
	// Training/OOS days have no bars at all (bars exist for evaluation days only), day 11 has none either.
	if want := 16 + 8 + 1; missing != want {
		t.Errorf("rows without a bar = %d, want %d", missing, want)
	}
	if labeled != len(dec.Rows) {
		t.Errorf("labeled %d of %d", labeled, len(dec.Rows))
	}
	// OOSUsed is the complete, labeled rows whose label ended by oos_end: days 1, 3, 6 and 7
	// (the independence check reads it only for rows inside the OOS window).
	if len(dec.OOSUsed) != 16 {
		t.Errorf("OOSUsed = %d, want 16", len(dec.OOSUsed))
	}
}
