package evalflow_test

import (
	"context"
	"encoding/json"
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

	state := jev.StateFromSnapshot(snap)
	if !reflect.DeepEqual(state.OrderFlow, snap.Feature.OrderFlowFeatures) {
		t.Fatalf("state.OrderFlow = %+v, want the snapshot's %+v", state.OrderFlow, snap.Feature.OrderFlowFeatures)
	}
	// A jittering OFI must not change the state Jev sees (and hashes for
	// FR-SCAN-2's unchanged-state suppression).
	bare := jev.StateFromSnapshot(domain.Snapshot{Symbol: "7203", Price: 2100, Timestamp: snap.Timestamp})
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
