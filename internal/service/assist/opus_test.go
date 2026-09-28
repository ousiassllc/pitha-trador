package assist_test

import (
	"encoding/json"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/assist"
)

func TestOpus_Review_ApprovesWhenExpectancyImprovesAndDrawdownUnchanged(t *testing.T) {
	opus := assist.NewOpus()
	approved, reviewJSON, err := opus.Review(assist.BacktestComparison{
		BaselineExpectancy: 0.20, CandidateExpectancy: 0.25,
		BaselineMaxDrawdownPct: 5.0, CandidateMaxDrawdownPct: 5.0,
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if !approved {
		t.Fatalf("Review() approved = false, want true (%s)", reviewJSON)
	}
	var review assist.OpusReview
	if err := json.Unmarshal([]byte(reviewJSON), &review); err != nil {
		t.Fatalf("unmarshal review json: %v", err)
	}
	if !review.Approved || !review.ExpectancyNotWorse || !review.MaxDrawdownWithinBudget {
		t.Fatalf("review = %+v, want every field true", review)
	}
}

func TestOpus_Review_ApprovesAtExactlyTenPercentDrawdownDegradation(t *testing.T) {
	opus := assist.NewOpus()
	approved, _, err := opus.Review(assist.BacktestComparison{
		BaselineExpectancy: 0.20, CandidateExpectancy: 0.20,
		BaselineMaxDrawdownPct: 10.0, CandidateMaxDrawdownPct: 11.0, // exactly +10% relative
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if !approved {
		t.Fatalf("Review() at exactly 10%% drawdown degradation = false, want true (boundary is inclusive)")
	}
}

func TestOpus_Review_RejectsWhenExpectancyWorsens(t *testing.T) {
	opus := assist.NewOpus()
	approved, reviewJSON, err := opus.Review(assist.BacktestComparison{
		BaselineExpectancy: 0.20, CandidateExpectancy: 0.15,
		BaselineMaxDrawdownPct: 5.0, CandidateMaxDrawdownPct: 5.0,
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if approved {
		t.Fatalf("Review() approved = true, want false when Expectancy worsens (%s)", reviewJSON)
	}
}

func TestOpus_Review_RejectsWhenDrawdownDegradesBeyondTenPercent(t *testing.T) {
	opus := assist.NewOpus()
	approved, reviewJSON, err := opus.Review(assist.BacktestComparison{
		BaselineExpectancy: 0.20, CandidateExpectancy: 0.25,
		BaselineMaxDrawdownPct: 10.0, CandidateMaxDrawdownPct: 11.5, // +15% relative
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if approved {
		t.Fatalf("Review() approved = true, want false when Max Drawdown degrades beyond 10%% (%s)", reviewJSON)
	}
}

func TestOpus_Review_RejectsAnyDrawdownWhenBaselineHadNone(t *testing.T) {
	opus := assist.NewOpus()
	approved, _, err := opus.Review(assist.BacktestComparison{
		BaselineExpectancy: 0.20, CandidateExpectancy: 0.25,
		BaselineMaxDrawdownPct: 0.0, CandidateMaxDrawdownPct: 0.1,
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if approved {
		t.Fatalf("Review() approved = true, want false when baseline had zero drawdown budget")
	}
}
