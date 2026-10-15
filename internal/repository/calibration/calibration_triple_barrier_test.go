package calibration_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Issue #756: the triple-barrier columns round-trip through Insert/Get.
func TestCalibrationRepository_TripleBarrierColumnsRoundTrip(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	decision := insertTraderDecision(t, decisions, instID, time.Now().UTC(), domain.JevDirectionLong)

	created, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 5, FutureReturn: 0.41, WasDirectionCorrect: ptr(true),
		BarrierHit: domain.BarrierHitTakeProfit, NetReturnBps: ptr(27.5), EntryRefPrice: ptr(2501.5),
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := outcomes.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BarrierHit != domain.BarrierHitTakeProfit || got.LabelVersion != domain.CalibrationLabelVersionTripleBarrier {
		t.Errorf("Get() = %+v, want BarrierHit=tp LabelVersion=2", got)
	}
	if got.NetReturnBps == nil || *got.NetReturnBps != 27.5 || got.EntryRefPrice == nil || *got.EntryRefPrice != 2501.5 {
		t.Errorf("Get() NetReturnBps/EntryRefPrice = %v/%v, want 27.5/2501.5", got.NetReturnBps, got.EntryRefPrice)
	}
}

// Issue #756: a row inserted without the new fields is a legacy close-only
// label: version 1, no barrier, NULL net return and entry price.
func TestCalibrationRepository_InsertWithoutTripleBarrierFieldsIsLegacyLabel(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	decision := insertTraderDecision(t, decisions, instID, time.Now().UTC(), domain.JevDirectionLong)

	created, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 5, FutureReturn: 0.41, WasDirectionCorrect: ptr(true),
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if created.LabelVersion != domain.CalibrationLabelVersionClose {
		t.Errorf("Insert() LabelVersion = %d, want %d", created.LabelVersion, domain.CalibrationLabelVersionClose)
	}

	got, err := outcomes.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.BarrierHit != "" || got.NetReturnBps != nil || got.EntryRefPrice != nil || got.LabelVersion != domain.CalibrationLabelVersionClose {
		t.Errorf("Get() = %+v, want legacy label (no barrier/net return/entry price, version 1)", got)
	}
}

// Issue #756: an unknown barrier_hit is rejected by the CHECK constraint.
func TestCalibrationRepository_InsertRejectsUnknownBarrierHit(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	decision := insertTraderDecision(t, decisions, instID, time.Now().UTC(), domain.JevDirectionLong)

	if _, err := outcomes.Insert(context.Background(), domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 5, BarrierHit: "bogus",
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
	}); err == nil {
		t.Fatal("Insert with barrier_hit=bogus succeeded, want CHECK constraint error")
	}
}

// Issue #756: ListLabeledSamples / ListLabeledSamplesSince expose the new
// columns so aggregations can tell legacy and triple-barrier labels apart.
func TestCalibrationRepository_ListLabeledSamples_CarriesTripleBarrierColumns(t *testing.T) {
	outcomes, decisions, instID := newCalibrationFixtures(t)
	ctx := context.Background()
	now := time.Now().UTC()
	decision := insertTraderDecision(t, decisions, instID, now, domain.JevDirectionLong)

	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 5, FutureReturn: 0.1, WasDirectionCorrect: ptr(true),
	}); err != nil {
		t.Fatalf("seed legacy outcome: %v", err)
	}
	if _, err := outcomes.Insert(ctx, domain.CalibrationOutcome{
		JevDecisionID: decision.ID, HorizonMinutes: 10, FutureReturn: -0.3, WasDirectionCorrect: ptr(false),
		BarrierHit: domain.BarrierHitStopLoss, NetReturnBps: ptr(-35.0), EntryRefPrice: ptr(1000.0),
		LabelVersion: domain.CalibrationLabelVersionTripleBarrier,
	}); err != nil {
		t.Fatalf("seed triple-barrier outcome: %v", err)
	}

	check := func(name string, samples []domain.LabeledSample) {
		t.Helper()
		byHorizon := map[int]domain.LabeledSample{}
		for _, s := range samples {
			byHorizon[s.HorizonMinutes] = s
		}
		legacy, tb := byHorizon[5], byHorizon[10]
		if legacy.LabelVersion != domain.CalibrationLabelVersionClose || legacy.BarrierHit != "" || legacy.NetReturnBps != nil || legacy.EntryRefPrice != nil {
			t.Errorf("%s: legacy sample = %+v, want version 1 with no barrier/net return/entry price", name, legacy)
		}
		if tb.LabelVersion != domain.CalibrationLabelVersionTripleBarrier || tb.BarrierHit != domain.BarrierHitStopLoss ||
			tb.NetReturnBps == nil || *tb.NetReturnBps != -35.0 || tb.EntryRefPrice == nil || *tb.EntryRefPrice != 1000.0 {
			t.Errorf("%s: triple-barrier sample = %+v, want version 2 / sl / -35 bps / 1000", name, tb)
		}
	}

	all, err := outcomes.ListLabeledSamples(ctx, nil)
	if err != nil {
		t.Fatalf("ListLabeledSamples: %v", err)
	}
	check("ListLabeledSamples", all)

	since, err := outcomes.ListLabeledSamplesSince(ctx, now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatalf("ListLabeledSamplesSince: %v", err)
	}
	check("ListLabeledSamplesSince", since)
}
