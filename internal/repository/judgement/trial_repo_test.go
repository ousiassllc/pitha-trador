package judgement_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
)

func openTrialRepo(t *testing.T) (*judgement.TrialRepository, int64) {
	t.Helper()
	db := newTestDB(t)
	instID := insertInstrument(t, db, "7203", "トヨタ自動車")
	live, err := judgement.NewDecisionRepository(db).Insert(context.Background(), domain.JevDecision{
		InstrumentID: instID, Symbol: "7203", Timestamp: time.Date(2026, 11, 13, 1, 0, 0, 0, time.UTC),
		DecisionType: domain.JevDecisionTypeTrader, StateHash: "h", StateJSON: "{}", QuestionVersion: "trader-v3", ResponseJSON: "{}", ModelID: "jev-1.13.0",
	})
	if err != nil {
		t.Fatalf("seed live decision: %v", err)
	}
	return judgement.NewTrialRepository(db), live.ID
}

func okRow(decisionID int64, arm string) jevtrial.Decision {
	return jevtrial.Decision{
		JevDecisionID: decisionID, ArmID: arm, PromptVersion: "trader-v3", Provider: "openrouter", RequestedModel: "cloudflare/clef-flash",
		Status: jevtrial.StatusOK, Direction: ptr("LONG"), Regime: ptr("TREND"), EntryQuality: ptr("good"),
		ToxicFlow: ptr(0.1), LiquidityStressed: ptr(0.2), ContinuationProbability: ptr(0.6),
		CostUSD: ptr(0.0001), CostSource: jevtrial.CostReported, CountedCostUSD: 0.0001,
	}
}

func TestTrialRepository_InsertGetRoundTripsNullsAndDefaults(t *testing.T) {
	repo, decisionID := openTrialRepo(t)
	ctx := context.Background()

	ok, err := repo.Insert(ctx, okRow(decisionID, "arm-a"))
	if err != nil {
		t.Fatalf("Insert ok row: %v", err)
	}
	got, err := repo.Get(ctx, ok.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LLMCutoffStatus != jevtrial.CutoffUnverified || got.RAGIncludesLiveModelAnswers || got.TruncationSuspected {
		t.Errorf("defaults = cutoff %q rag %v truncation %v, want unverified/false/false", got.LLMCutoffStatus, got.RAGIncludesLiveModelAnswers, got.TruncationSuspected)
	}
	if got.Confidence != nil || got.InputTokens != nil || got.HTTPStatus != nil || got.ResponseJSON != nil {
		t.Errorf("unreported values must stay NULL: confidence %v tokens %v http %v response %v", got.Confidence, got.InputTokens, got.HTTPStatus, got.ResponseJSON)
	}
	if got.Direction == nil || *got.Direction != "LONG" || got.CostUSD == nil || *got.CostUSD != 0.0001 {
		t.Errorf("Get() = %+v, want direction LONG and cost 0.0001", got)
	}

	failed := jevtrial.Decision{
		JevDecisionID: decisionID, ArmID: "arm-b", PromptVersion: "trader-v3", Provider: "openrouter", RequestedModel: "google/gemini-decision-1",
		Status: jevtrial.StatusFailed, FailureCode: jevtrial.FailureHTTP402, HTTPStatus: ptr(402), ErrorExcerpt: "insufficient credits",
		CostSource: jevtrial.CostUnknown,
	}
	row, err := repo.Insert(ctx, failed)
	if err != nil {
		t.Fatalf("Insert failed row: %v", err)
	}
	got, err = repo.Get(ctx, row.ID)
	if err != nil {
		t.Fatalf("Get failed row: %v", err)
	}
	if got.Direction != nil || got.CostUSD != nil || got.CostSource != jevtrial.CostUnknown || got.HTTPStatus == nil || *got.HTTPStatus != 402 {
		t.Errorf("failed row = %+v, want no answers, NULL cost (not 0), unknown source and status 402", got)
	}
}

func TestTrialRepository_OneRowPerDecisionAndArmAndRejectsInconsistentRows(t *testing.T) {
	repo, decisionID := openTrialRepo(t)
	ctx := context.Background()
	if _, err := repo.Insert(ctx, okRow(decisionID, "arm-a")); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := repo.Insert(ctx, okRow(decisionID, "arm-b")); err != nil {
		t.Errorf("a second arm for one decision was refused: %v", err)
	}
	if _, err := repo.Insert(ctx, okRow(decisionID, "arm-a")); err == nil {
		t.Error("a second row for the same (decision, arm) was accepted")
	}
	if _, err := repo.Insert(ctx, okRow(decisionID+99, "arm-c")); err == nil {
		t.Error("a row for an unknown live decision was accepted (FK)")
	}
	noAnswer := okRow(decisionID, "arm-d")
	noAnswer.Direction = nil
	if _, err := repo.Insert(ctx, noAnswer); err == nil {
		t.Error("an ok row without a direction was accepted: a refused or missing answer is partial/refused")
	}
	failedWithoutCode := okRow(decisionID, "arm-e")
	failedWithoutCode.Status = jevtrial.StatusFailed
	if _, err := repo.Insert(ctx, failedWithoutCode); err == nil {
		t.Error("a failed row without a failure_code was accepted")
	}
	partial := okRow(decisionID, "arm-f")
	partial.Status, partial.EntryQuality = jevtrial.StatusPartial, nil
	if _, err := repo.Insert(ctx, partial); err != nil {
		t.Errorf("a partial row with a refused (NULL) answer was refused: %v", err)
	}
}

func TestTrialRepository_SpentSumsTheCountedCostPerArmAndPerDay(t *testing.T) {
	repo, decisionID := openTrialRepo(t)
	ctx := context.Background()
	day := time.Date(2026, 11, 13, 3, 0, 0, 0, time.UTC)
	insert := func(arm string, at time.Time, cost float64) {
		t.Helper()
		row := okRow(decisionID, arm)
		row.CreatedAt, row.CountedCostUSD = at, cost
		if _, err := repo.Insert(ctx, row); err != nil {
			t.Fatalf("Insert %s: %v", arm, err)
		}
	}
	insert("arm-a", day, 0.25)
	insert("arm-b", day.Add(time.Hour), 0.5)
	insert("arm-c", day.Add(48*time.Hour), 9) // another day

	from, to := day.Add(-time.Hour), day.Add(24*time.Hour)
	all, err := repo.SpentBetween(ctx, from, to)
	if err != nil || all.Calls != 2 || all.CountedCostUSD != 0.75 {
		t.Errorf("SpentBetween = %+v (err %v), want 2 calls, 0.75", all, err)
	}
	one, err := repo.SpentByArmBetween(ctx, "arm-b", from, to)
	if err != nil || one.Calls != 1 || one.CountedCostUSD != 0.5 {
		t.Errorf("SpentByArmBetween(arm-b) = %+v (err %v), want 1 call, 0.5", one, err)
	}
}
