package metalabel_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

func TestRepository_AdoptionRecordRoundTrips(t *testing.T) {
	r := metalabel.NewRepository(newTestDB(t))
	ctx := context.Background()
	m := sampleModel("v-adopt", metalabel.ModelRejected, time.Date(2026, 10, 14, 17, 0, 0, 0, time.UTC))
	m.RejectReason = "candidate_not_better_than_active"
	m.OOSTakes, m.OOSDays = 430, 5
	m.OOSEVCI = &metalabel.Interval{Low: 0.4, High: 7.5}
	m.OOSAUCCI = &metalabel.Interval{Low: 0.51, High: 0.58}
	m.Adoption = metalabel.Adoption{
		Criteria:   metalabel.Criteria{MinOOSSamples: 848, MinTakes: 424, MinOOSDays: 4, MinAUCLower: 0.5, Alpha: 0.025, Resamples: 1000, Seed: 42},
		Comparison: &metalabel.Comparison{ActiveVersion: "v-old", Comparable: true, PairedSamples: 900, DiffBps: -0.2, DiffCI: &metalabel.Interval{Low: -1, High: 0.6}},
		Gate:       &metalabel.Gate{Reason: "model_swap_rate_limited", Detail: "2 of 5", Until: "2026-10-21T15:00:00Z"},
	}
	saved, err := r.InsertModel(ctx, m)
	if err != nil {
		t.Fatalf("InsertModel: %v", err)
	}

	got, found, err := r.LatestModel(ctx)
	if err != nil || !found {
		t.Fatalf("LatestModel = found %v, err %v", found, err)
	}
	if got.OOSTakes != 430 || got.OOSDays != 5 || got.OOSEVCI == nil || *got.OOSEVCI != *m.OOSEVCI || got.OOSAUCCI == nil || *got.OOSAUCCI != *m.OOSAUCCI {
		t.Errorf("OOS counts/intervals = %d %d %v %v", got.OOSTakes, got.OOSDays, got.OOSEVCI, got.OOSAUCCI)
	}
	if got.Adoption.Criteria != m.Adoption.Criteria || got.Adoption.Comparison == nil || got.Adoption.Comparison.ActiveVersion != "v-old" ||
		got.Adoption.Comparison.DiffCI == nil || got.Adoption.Gate == nil || got.Adoption.Gate.Reason != "model_swap_rate_limited" {
		t.Errorf("Adoption = %+v", got.Adoption)
	}
	if got.Forward.EvaluatedAt != "" {
		t.Errorf("Forward = %+v, want empty before the first forward evaluation", got.Forward)
	}

	// The forward evaluation is stored without touching the status or the adoption record.
	ev := 1.5
	forward := metalabel.Forward{EvaluatedAt: "2026-10-20T09:00:00Z", From: "2026-10-14T17:00:00Z", Samples: 300, Takes: 120, Days: 3, EVBps: &ev, EVCI: &metalabel.Interval{Low: -2, High: 5}}
	if err := r.UpdateForward(ctx, saved.ID, forward); err != nil {
		t.Fatalf("UpdateForward: %v", err)
	}
	got, _, err = r.LatestModel(ctx)
	if err != nil {
		t.Fatalf("LatestModel: %v", err)
	}
	if got.Forward.Takes != 120 || got.Forward.EVBps == nil || *got.Forward.EVBps != 1.5 || got.Forward.EVCI == nil || got.Forward.EVCI.High != 5 {
		t.Errorf("Forward = %+v", got.Forward)
	}
	if got.Status != metalabel.ModelRejected || got.Adoption.Criteria.MinTakes != 424 {
		t.Errorf("UpdateForward changed the status or adoption record: %+v", got)
	}
	if err := r.UpdateForward(ctx, saved.ID+99, forward); err == nil {
		t.Error("UpdateForward of an unknown model succeeded")
	}
}
