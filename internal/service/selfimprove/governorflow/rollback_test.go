package governorflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/selfimprove"
)

// TestGovernor_TrackAndRollback pins FR-SELFIMPROVE-6: rollback happens only
// on a >=20% relative worsening (measured against |pre|, so negative/zero
// baselines work), and never when post >= pre or when either window has no
// closed position (nil pnl) - that case is indeterminate.
func TestGovernor_TrackAndRollback(t *testing.T) {
	f64 := func(v float64) *float64 { return &v }
	tests := []struct {
		name         string
		pre, post    *float64
		wantRollback bool
	}{
		{"positive pre then post worse by 30 pct", f64(1000), f64(700), true},
		{"positive pre then post improved", f64(500), f64(900), false},
		{"negative pre then post greatly improved", f64(-100), f64(-10), false},
		{"negative pre then post unchanged", f64(-100), f64(-100), false},
		{"negative pre then post worse by less than 20 pct", f64(-100), f64(-110), false},
		{"negative pre then post worse by more than 20 pct", f64(-100), f64(-130), true},
		{"zero pre then post slightly negative", f64(0), f64(-0.01), true},
		{"zero pre then post zero", f64(0), f64(0), false},
		{"positive pre then no closed position post-apply", f64(500), nil, false},
		{"no closed position pre-apply  negative post", nil, f64(-50), false},
		{"no closed position in either window", nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newGovernorFixtures(t)
			ctx := context.Background()
			appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)
			trackingClosed := appliedAt.Add(10 * 24 * time.Hour) // safely past the ~7-calendar-day (5 business day) window

			if tt.pre != nil {
				f.closePosition(t, appliedAt.Add(-3*24*time.Hour), *tt.pre)
			}
			if tt.post != nil {
				f.closePosition(t, appliedAt.Add(3*24*time.Hour), *tt.post)
			}

			changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
				{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
			})
			if err != nil {
				t.Fatalf("EncodePolicyChanges: %v", err)
			}
			if err := f.settings.Set(ctx, domain.PolicyKeyLongMinProbability, `0.65`, appliedAt); err != nil {
				t.Fatalf("seed applied setting: %v", err)
			}
			proposal, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
			if err != nil {
				t.Fatalf("Insert proposal: %v", err)
			}
			if err := f.proposals.MarkApplied(ctx, proposal.ID, "sol-1", appliedAt); err != nil {
				t.Fatalf("MarkApplied: %v", err)
			}

			g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
				fakeShadowBacktestSource{}, baselinePolicyConfig(0.60),
				selfimprove.WithNow(func() time.Time { return trackingClosed }))

			rolledBack, err := g.TrackAndRollback(ctx, proposal.ID)
			if err != nil {
				t.Fatalf("TrackAndRollback: %v", err)
			}
			if rolledBack != tt.wantRollback {
				t.Fatalf("TrackAndRollback() = %v, want %v", rolledBack, tt.wantRollback)
			}

			got, err := f.proposals.Get(ctx, proposal.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			wantStatus, wantValue := domain.PolicyProposalStatusApplied, "0.65"
			if tt.wantRollback {
				wantStatus, wantValue = domain.PolicyProposalStatusRolledBack, "0.60" // the pre-apply value
				if got.RolledBackReason == nil || *got.RolledBackReason == "" {
					t.Fatalf("Get().RolledBackReason = %v, want a nonempty reason", got.RolledBackReason)
				}
			}
			if got.Status != wantStatus {
				t.Fatalf("Get().Status = %q, want %q", got.Status, wantStatus)
			}
			raw, ok, err := f.settings.Get(ctx, domain.PolicyKeyLongMinProbability)
			if err != nil {
				t.Fatalf("settings.Get: %v", err)
			}
			if !ok || raw != wantValue {
				t.Fatalf("runtime_settings[%s] = (%q, %v), want (%s, true)", domain.PolicyKeyLongMinProbability, raw, ok, wantValue)
			}
		})
	}
}

func TestGovernor_TrackAndRollback_NoActionBeforeWindowCloses(t *testing.T) {
	f := newGovernorFixtures(t)
	ctx := context.Background()
	appliedAt := time.Date(2026, 3, 2, 16, 0, 0, 0, time.UTC)

	changesJSON, err := domain.EncodePolicyChanges([]domain.PolicyChange{
		{Key: domain.PolicyKeyLongMinProbability, OldValue: `0.60`, NewValue: `0.65`},
	})
	if err != nil {
		t.Fatalf("EncodePolicyChanges: %v", err)
	}
	proposal, err := f.proposals.Insert(ctx, domain.PolicyProposal{RationaleJSON: `{}`, ProposedChangesJSON: changesJSON})
	if err != nil {
		t.Fatalf("Insert proposal: %v", err)
	}
	if err := f.proposals.MarkApplied(ctx, proposal.ID, "sol-1", appliedAt); err != nil {
		t.Fatalf("MarkApplied: %v", err)
	}

	baseline := baselinePolicyConfig(0.60)
	g := selfimprove.NewGovernor(f.proposals, f.settings, f.positions,
		fakeShadowBacktestSource{}, baseline,
		selfimprove.WithNow(func() time.Time { return appliedAt.Add(24 * time.Hour) })) // well inside the tracking window

	rolledBack, err := g.TrackAndRollback(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("TrackAndRollback: %v", err)
	}
	if rolledBack {
		t.Fatalf("TrackAndRollback() = true, want false while the tracking window has not yet closed")
	}

	got, err := f.proposals.Get(ctx, proposal.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != domain.PolicyProposalStatusApplied {
		t.Fatalf("Get().Status = %q, want still applied", got.Status)
	}
}
