package jevflow_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
)

// optionNames returns the option ids of the choice question id in req.
func optionNames(t *testing.T, req jevRequest, id string) []string {
	t.Helper()
	var q struct {
		Type     string            `json:"type"`
		Criteria map[string]string `json:"criteria"`
	}
	if err := json.Unmarshal(req.Questions[id], &q); err != nil || q.Type != "choice" {
		t.Fatalf("question %q = %s (%v), want a choice question", id, req.Questions[id], err)
	}
	names := make([]string, 0, len(q.Criteria))
	for name := range q.Criteria {
		names = append(names, name)
	}
	return names
}

func TestSol_Analyze_JevChoosesAmongCodeGeneratedCandidates(t *testing.T) {
	const pick = "policy.long.min_probability=0.65"
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any {
		return map[string]any{"best_change": jevtest.Choice(pick, 0.7)}
	})
	sol := assist.NewSol(noOverride("sol"), assist.WithJev(f.Client))

	proposal, ok, err := sol.Analyze(context.Background(), solInput())
	if err != nil || !ok {
		t.Fatalf("Analyze = (%+v, %v, %v), want a proposal", proposal, ok, err)
	}
	if len(proposal.Changes) != 1 || proposal.Changes[0].Key != domain.PolicyKeyLongMinProbability || string(proposal.Changes[0].NewValue) != "0.65" {
		t.Errorf("Changes = %+v, want policy.long.min_probability -> 0.65 (the chosen candidate, value from code)", proposal.Changes)
	}
	var rationale struct {
		Source     string  `json:"source"`
		Selected   string  `json:"selected"`
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(proposal.RationaleJSON), &rationale); err != nil || rationale.Source != "jev" || rationale.Selected != pick || rationale.Confidence != 0.7 {
		t.Errorf("RationaleJSON = %s (%v), want the code-built jev rationale", proposal.RationaleJSON, err)
	}
	if n := len(f.calls()); n != 1 {
		t.Errorf("Jev calls = %d, want 1", n)
	}
}

func TestSol_Analyze_EveryJevCandidatePassesTheGovernorChecks(t *testing.T) {
	var options []string
	f := newFakeJev(t, "jev-key", func(req jevRequest) map[string]any {
		options = optionNames(t, req, "best_change")
		return map[string]any{"best_change": jevtest.Choice("none", 0.5)}
	})
	in := solInput()
	in.Short = in.Long
	in.Short.Thresholds.MaxToxicFlow = 0.98 // near the top of the range: +0.05 is clamped to 1.00
	sol := assist.NewSol(noOverride("sol"), assist.WithJev(f.Client))
	if _, ok, err := sol.Analyze(context.Background(), in); err != nil || ok {
		t.Fatalf("Analyze(none) = ok %v, err %v, want no proposal", ok, err)
	}
	if len(options) < 4 {
		t.Fatalf("options = %v, want several candidates plus none", options)
	}

	for _, option := range options {
		if option == "none" {
			continue
		}
		key, value, _ := strings.Cut(option, "=")
		raw := value
		if key == domain.PolicyKeyLongMinEntryQuality || key == domain.PolicyKeyShortMinEntryQuality {
			raw = `"` + value + `"`
		}
		old := oldValueOf(in, key)
		if err := domain.ValidatePolicyChanges([]domain.PolicyChange{{Key: key, OldValue: old, NewValue: raw}}); err != nil {
			t.Errorf("candidate %q fails FR-SELFIMPROVE-2/3: %v", option, err)
		}
	}
}

// oldValueOf returns key's current threshold in solInput's JSON convention.
func oldValueOf(in assist.SolAnalysisInput, key string) string {
	dir := in.Long
	if strings.HasPrefix(key, "policy.short.") {
		dir = in.Short
	}
	th := dir.Thresholds
	var v any
	switch strings.TrimPrefix(strings.TrimPrefix(key, "policy.long."), "policy.short.") {
	case "min_probability":
		v = th.MinProbability
	case "min_entry_quality":
		v = th.MinEntryQuality
	case "min_continuation_probability":
		v = th.MinContinuationProbability
	case "max_toxic_flow":
		v = th.MaxToxicFlow
	case "max_liquidity_stressed":
		v = th.MaxLiquidityStressed
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSol_Analyze_JevNoneAndNoSamplesProposeNothing(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any {
		return map[string]any{"best_change": jevtest.Choice("none", 0.9)}
	})
	sol := assist.NewSol(noOverride("sol"), assist.WithJev(f.Client))
	if _, ok, err := sol.Analyze(context.Background(), solInput()); err != nil || ok {
		t.Errorf("Analyze with Jev answering none = ok %v, err %v, want no proposal and no error", ok, err)
	}

	before := len(f.calls())
	if _, ok, err := sol.Analyze(context.Background(), assist.SolAnalysisInput{}); err != nil || ok {
		t.Errorf("Analyze without samples = ok %v, err %v, want no proposal and no error", ok, err)
	}
	if len(f.calls()) != before {
		t.Error("Jev was asked although no candidate exists")
	}
}

func TestSol_Analyze_JevFailureIsReturnedSoTheDayIsSkipped(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any { return nil })
	sol := assist.NewSol(noOverride("sol"), assist.WithJev(f.Client))
	if _, ok, err := sol.Analyze(context.Background(), solInput()); err == nil || ok {
		t.Errorf("Analyze against a failing Jev = ok %v, err %v, want an error", ok, err)
	}
}

func TestOpus_Review_JevNoulAdoptionProbabilityDecides(t *testing.T) {
	for _, tc := range []struct {
		name     string
		noul     float64
		approved bool
	}{
		{"high probability approves", 0.82, true},
		{"exactly at threshold approves", 0.5, true},
		{"low probability rejects", 0.31, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any {
				return map[string]any{"adopt": jevtest.Noul(tc.noul)}
			})
			opus := assist.NewOpus(noOverride("opus"), assist.WithJev(f.Client))

			approved, reviewJSON, err := opus.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5))
			if err != nil || approved != tc.approved {
				t.Fatalf("Review = (%v, %s, %v), want approved=%v", approved, reviewJSON, err, tc.approved)
			}
			var review assist.OpusReview
			if err := json.Unmarshal([]byte(reviewJSON), &review); err != nil || !review.LLMReviewed || review.Approved != tc.approved || review.Reason == "" {
				t.Errorf("review = %+v (%v), want an LLM-reviewed verdict with a reason", review, err)
			}
			var q struct {
				Type string `json:"type"`
			}
			reqs := f.calls()
			if len(reqs) != 1 {
				t.Fatalf("Jev calls = %d, want 1", len(reqs))
			}
			_ = json.Unmarshal(reqs[0].Questions["adopt"], &q)
			if q.Type != "noul" {
				t.Errorf("question type = %q, want noul", q.Type)
			}
		})
	}
}

func TestOpus_Review_JevIsNotAskedWhenDeterministicChecksFail(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any {
		return map[string]any{"adopt": jevtest.Noul(0.99)}
	})
	opus := assist.NewOpus(noOverride("opus"), assist.WithJev(f.Client))

	approved, _, err := opus.Review(context.Background(), reviewInput(0.30, 0.10, 5, 5)) // expectancy worsened
	if err != nil || approved {
		t.Fatalf("Review = (%v, %v), want a deterministic reject", approved, err)
	}
	if n := len(f.calls()); n != 0 {
		t.Errorf("Jev was asked %d time(s); it may only veto a deterministic pass, never rescue a miss", n)
	}
}

func TestOpus_Review_JevFailureIsAnErrorNotAnApproval(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(jevRequest) map[string]any { return nil })
	opus := assist.NewOpus(noOverride("opus"), assist.WithJev(f.Client))
	if approved, _, err := opus.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5)); err == nil || approved {
		t.Errorf("Review against a failing Jev = (%v, %v), want an error and no approval", approved, err)
	}
}

func TestRoles_OverrideIsPerRoleAndUnsetRolesFallBackToJev(t *testing.T) {
	f := newFakeJev(t, "jev-key", func(req jevRequest) map[string]any {
		if _, ok := req.Questions["best_change"]; ok {
			return map[string]any{"best_change": jevtest.Choice("none", 0.5)}
		}
		return map[string]any{"adopt": jevtest.Noul(0.9)}
	})
	// Opus is overridden by an unreachable API: the override wins and fails
	// visibly instead of silently falling back to Jev.
	opus := assist.NewOpus(assist.NewClient(assist.Config{Label: "opus", BaseURL: "http://127.0.0.1:1", MaxAttempts: 1}), assist.WithJev(f.Client))
	if _, _, err := opus.Review(context.Background(), reviewInput(0.20, 0.25, 5, 5)); err == nil {
		t.Error("Opus with an unreachable override fell back to Jev")
	}
	if n := len(f.calls()); n != 0 {
		t.Errorf("Jev calls = %d, want 0 while OPUS_BASE_URL is set", n)
	}

	// Sol has no override, so it uses Jev.
	sol := assist.NewSol(noOverride("sol"), assist.WithJev(f.Client))
	if _, _, err := sol.Analyze(context.Background(), solInput()); err != nil || len(f.calls()) != 1 {
		t.Errorf("Sol without override: err %v, Jev calls %d; want one Jev call", err, len(f.calls()))
	}
}
