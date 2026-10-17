// Package costflow_test checks that the triple-barrier label cost - and so the
// meta-labeling model's expected value, built from the labels' net returns -
// follows the Settings cost settings (issue #779). It lives in its own
// directory to keep internal/service/calibration within the linterly budget.
package costflow_test

import (
	"context"
	"encoding/json"
	"math"
	"math/rand"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/service/calibration"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
)

func ptr[T any](v T) *T { return &v }

type settingsStore map[string]string

func (s settingsStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s[key]
	return v, ok, nil
}

var base = time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)

// labelNet labels one decision of direction whose path ends grossBps in its
// favour (never reaching a barrier) under the cost settings in store, and
// returns the stored net return.
func labelNet(t *testing.T, direction string, grossBps float64, store settingsStore) float64 {
	t.Helper()
	ctx := context.Background()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	inst, err := market.NewInstrumentRepository(db).Create(ctx, domain.Instrument{Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	decisions, snapshots, outcomes := judgement.NewDecisionRepository(db), market.NewSnapshotRepository(db), calrepo.NewCalibrationRepository(db)
	decision, err := decisions.Insert(ctx, domain.JevDecision{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: base, DecisionType: domain.JevDecisionTypeTrader,
		StateHash: "hash", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}",
		Direction: ptr(direction), Confidence: ptr(0.8), ModelID: "test-model", CreatedAt: base,
	})
	if err != nil {
		t.Fatalf("insert decision: %v", err)
	}
	factor := 1.0
	if direction == domain.JevDirectionShort {
		factor = -1
	}
	// The entry quote is 1000 on both sides; the path ends grossBps in favour.
	end := 1000 * (1 + factor*grossBps/10000)
	var bars []domain.Snapshot
	for offset, p := range map[time.Duration]float64{0: 1000, 2 * time.Minute: end, 5 * time.Minute: end} {
		bars = append(bars, domain.Snapshot{InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: base.Add(offset), Price: p, Bid: ptr(p), Ask: ptr(p)})
	}
	if _, err := snapshots.InsertBatch(ctx, bars); err != nil {
		t.Fatalf("insert snapshots: %v", err)
	}
	labeler := calibration.NewLabeler(decisions, snapshots, outcomes, calibration.WithCostSettings(store))
	payload, _ := json.Marshal(calrepo.OutcomeLabelJobPayload{JevDecisionID: decision.ID, HorizonMinutes: 5})
	if err := labeler.HandleJob(ctx, jobqueue.Job{PayloadJSON: string(payload)}); err != nil {
		t.Fatalf("HandleJob: %v", err)
	}
	got, err := outcomes.ListByDecisionIDs(ctx, []int64{decision.ID})
	if err != nil || len(got) != 1 || got[0].NetReturnBps == nil || got[0].BarrierHit != domain.BarrierHitTimeout {
		t.Fatalf("stored outcomes = %+v, %v; want one timeout label with a net return", got, err)
	}
	return *got[0].NetReturnBps
}

// Changing a cost setting changes the label's net return by the change of the
// modelled round-trip cost, per direction; at the defaults the label is as it
// was before the settings existed (cost 10 bps).
func TestLabelCostFollowsTheCostSettings(t *testing.T) {
	if got := labelNet(t, domain.JevDirectionLong, 25, settingsStore{}); math.Abs(got-15) > 1e-6 {
		t.Fatalf("LONG net at the defaults = %v, want 25 - 10 = 15", got)
	}
	raised := settingsStore{
		costsettings.KeyFeeBps:                  "3", // +6 bps round trip, both directions
		costsettings.KeySlippageLongContinuous:  "4", // +4 bps round trip, LONG only
		costsettings.KeyShortBorrowBps:          "2", // SHORT only
		costsettings.KeySlippageShortContinuous: "2", // unchanged for SHORT
	}
	if got := labelNet(t, domain.JevDirectionLong, 25, raised); math.Abs(got-5) > 1e-6 {
		t.Errorf("LONG net with the raised costs = %v, want 25 - (10 + 6 + 4) = 5", got)
	}
	if got := labelNet(t, domain.JevDirectionShort, 25, raised); math.Abs(got-7) > 1e-6 {
		t.Errorf("SHORT net with the raised costs = %v, want 25 - (10 + 6 + 2) = 7", got)
	}
}

// sample builds a training row whose first feature separates winners and
// losers, so the logistic regression learns something and Train accepts it.
func sample(rng *rand.Rand, net float64) metalabel.Sample {
	v := make([]float64, len(metalabel.FeatureNames()))
	for i := range v {
		v[i] = rng.NormFloat64() * 0.3
	}
	if net > 0 {
		v[0]++
	} else {
		v[0]--
	}
	return metalabel.Sample{Vector: v, NetReturnBps: net}
}

// The meta-labeling EV is p*WinBps - (1-p)*LossBps over the labels' net returns,
// so raising the cost settings lowers the EV of the same decisions (#779).
func TestMetaLabelEVFollowsTheCostSettings(t *testing.T) {
	evAt := func(store settingsStore) float64 {
		winNet := labelNet(t, domain.JevDirectionLong, 25, store)  // a winning timeout
		lossNet := labelNet(t, domain.JevDirectionLong, -5, store) // a losing timeout (inside the stop-loss)
		if winNet <= 0 || lossNet >= 0 {
			t.Fatalf("win/loss net = %v/%v, want a win and a loss", winNet, lossNet)
		}
		rng := rand.New(rand.NewSource(7))
		var samples []metalabel.Sample
		for range 40 {
			samples = append(samples, sample(rng, winNet), sample(rng, lossNet))
		}
		model, err := metalabel.Train(samples[:60], samples[60:], metalabel.TrainOptions{Version: "t", Calibration: metalabel.CalibrationPlatt, LR: metalabel.DefaultTrainConfig()})
		if err != nil {
			t.Fatalf("Train: %v", err)
		}
		return model.ExpectedValueBps(0.6)
	}
	cheap := evAt(settingsStore{})
	costly := evAt(settingsStore{costsettings.KeyFeeBps: "3", costsettings.KeySlippageLongContinuous: "4"}) // +10 bps round trip
	// Winners earn 10 bps less and losers lose 10 bps more: EV(0.6) falls by 10.
	if math.Abs((cheap-costly)-10) > 1e-6 {
		t.Fatalf("EV = %v at the defaults vs %v with +10 bps cost, want it 10 bps lower", cheap, costly)
	}
}
