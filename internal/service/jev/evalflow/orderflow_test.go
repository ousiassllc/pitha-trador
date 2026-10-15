package evalflow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

func orderFlowFixture() domain.OrderFlowFeatures {
	return domain.OrderFlowFeatures{
		OFIShort: new(120.0), OFIMid: new(-80.0), OFILong: new(0.0),
		OFIWindowShortSeconds: new(int64(10)), OFIWindowMidSeconds: new(int64(30)), OFIWindowLongSeconds: new(int64(60)),
		MicropriceDevBps: new(-1.25),
	}
}

func TestStateFromSnapshot_CarriesOrderFlowButNeverSendsItToJev(t *testing.T) {
	snap := domain.Snapshot{Symbol: "7203", Price: 2100, Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)}
	snap.Feature.OrderFlowFeatures = orderFlowFixture()

	now := snap.Timestamp.Add(5 * time.Second)
	state := jev.StateFromSnapshot(snap, now)
	if !reflect.DeepEqual(state.OrderFlow, snap.Feature.OrderFlowFeatures) {
		t.Fatalf("state.OrderFlow = %+v, want the snapshot's %+v", state.OrderFlow, snap.Feature.OrderFlowFeatures)
	}
	// A jittering OFI must not change the state Jev sees (and hashes for
	// FR-SCAN-2's unchanged-state suppression).
	bare := jev.StateFromSnapshot(domain.Snapshot{Symbol: "7203", Price: 2100, Timestamp: snap.Timestamp}, now)
	if a, b := marshalState(t, state), marshalState(t, bare); a != b {
		t.Fatalf("OFI changed the Jev state JSON:\n%s\n%s", a, b)
	}
	if strings.Contains(marshalState(t, state), "ofi") {
		t.Fatalf("Jev state JSON contains OFI: %s", marshalState(t, state))
	}
}

func TestScout_Evaluate_PersistsOrderFlowOfTheDecisionMoment(t *testing.T) {
	server := scoutServer(t, jev.ScoutResponse{InterestingNow: 0.8, LiquidityOk: 0.9, AbnormalActivity: 0.6, ModelID: "m"})
	db := newTestDB(t)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203")
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL}), decisions, market.NewSnapshotRepository(db), nil,
		rag.NewService(db, decisions, market.NewSnapshotRepository(db)), testThresholds())

	run := func(flow domain.OrderFlowFeatures) domain.JevDecision {
		state := jev.ScoutState{Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC), Price: 2100, OrderFlow: flow}
		decision, _, err := scout.Evaluate(context.Background(), inst.ID, state)
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		saved, err := decisions.Get(context.Background(), decision.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		return saved
	}

	with, without := run(orderFlowFixture()), run(domain.OrderFlowFeatures{})
	if !reflect.DeepEqual(with.OrderFlow, orderFlowFixture()) {
		t.Errorf("saved OrderFlow = %+v, want %+v", with.OrderFlow, orderFlowFixture())
	}
	if !reflect.DeepEqual(without.OrderFlow, domain.OrderFlowFeatures{}) {
		t.Errorf("a decision without OFI read back %+v, want all NULL", without.OrderFlow)
	}
	if with.StateHash != without.StateHash {
		t.Error("OFI changed state_hash; FR-SCAN-2's suppression would break")
	}
}

func TestTrader_Evaluate_PersistsOrderFlowOfTheDecisionMoment(t *testing.T) {
	server := traderServer(t, jev.TraderResponse{
		Direction: domain.JevDirectionLong, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityGood,
		Confidence: 0.7, ModelID: "m",
	})
	db := newTestDB(t)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203")
	trader := jev.NewTrader(jev.NewClient(jev.Config{BaseURL: server.URL}), decisions,
		rag.NewService(db, decisions, market.NewSnapshotRepository(db)))

	state := jev.ScoutState{Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC), Price: 2100, OrderFlow: orderFlowFixture()}
	decision, err := trader.Evaluate(context.Background(), inst.ID, state)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	saved, err := decisions.Get(context.Background(), decision.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(saved.OrderFlow, orderFlowFixture()) {
		t.Errorf("saved OrderFlow = %+v, want %+v", saved.OrderFlow, orderFlowFixture())
	}
}

func marshalState(t *testing.T, s jev.ScoutState) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	return string(b)
}

// The state Jev sees carries data_age_ms (issue #754): the bar's age at the
// decision time, floored at zero, and a decision row persists the same value.
func TestStateFromSnapshot_DataAgeMS(t *testing.T) {
	ts := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	for name, tc := range map[string]struct {
		now  time.Time
		want int64
	}{
		"five seconds old": {ts.Add(5 * time.Second), 5000},
		"sub-second":       {ts.Add(1250 * time.Millisecond), 1250},
		"clock behind bar": {ts.Add(-time.Second), 0},
	} {
		state := jev.StateFromSnapshot(domain.Snapshot{Symbol: "7203", Price: 2100, Timestamp: ts}, tc.now)
		if state.DataAgeMS == nil || *state.DataAgeMS != tc.want {
			t.Errorf("%s: DataAgeMS = %v, want %d", name, state.DataAgeMS, tc.want)
		}
		if want := fmt.Sprintf(`"data_age_ms":%d`, tc.want); !strings.Contains(marshalState(t, state), want) {
			t.Errorf("%s: state JSON lacks %s: %s", name, want, marshalState(t, state))
		}
	}
}

func TestScoutAndTrader_Evaluate_PersistDataAge(t *testing.T) {
	db := newTestDB(t)
	decisions := judgement.NewDecisionRepository(db)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203")
	ragService := rag.NewService(db, decisions, market.NewSnapshotRepository(db))
	state := jev.ScoutState{Symbol: "7203", Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC), Price: 2100, DataAgeMS: new(int64(7300))}

	scoutSrv := scoutServer(t, jev.ScoutResponse{InterestingNow: 0.8, LiquidityOk: 0.9, AbnormalActivity: 0.6, ModelID: "m"})
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: scoutSrv.URL}), decisions, market.NewSnapshotRepository(db), nil, ragService, testThresholds())
	scoutDecision, _, err := scout.Evaluate(context.Background(), inst.ID, state)
	if err != nil {
		t.Fatalf("Scout.Evaluate: %v", err)
	}

	traderSrv := traderServer(t, jev.TraderResponse{
		Direction: domain.JevDirectionLong, Regime: domain.JevRegimeTrend, EntryQuality: domain.JevEntryQualityGood,
		Confidence: 0.7, ModelID: "m",
	})
	trader := jev.NewTrader(jev.NewClient(jev.Config{BaseURL: traderSrv.URL}), decisions, ragService)
	traderDecision, err := trader.Evaluate(context.Background(), inst.ID, state)
	if err != nil {
		t.Fatalf("Trader.Evaluate: %v", err)
	}

	for name, id := range map[string]int64{"scout": scoutDecision.ID, "trader": traderDecision.ID} {
		saved, err := decisions.Get(context.Background(), id)
		if err != nil {
			t.Fatalf("%s Get: %v", name, err)
		}
		if saved.DataAgeMS == nil || *saved.DataAgeMS != 7300 {
			t.Errorf("%s: saved DataAgeMS = %v, want 7300", name, saved.DataAgeMS)
		}
	}
}
