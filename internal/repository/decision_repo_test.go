package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
)

func openTestDecisionRepo(t *testing.T) (*repository.DecisionRepository, int64) {
	t.Helper()
	db := newTestDB(t)

	instruments := repository.NewInstrumentRepository(db)
	inst, err := instruments.Create(context.Background(), domain.Instrument{
		Symbol: "7203", Name: "トヨタ自動車", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("create instrument fixture: %v", err)
	}

	return repository.NewDecisionRepository(db), inst.ID
}

func TestDecisionRepository_InsertAndGet_ScoutDecision(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)

	created, err := repo.Insert(ctx, domain.JevDecision{
		InstrumentID:    instrumentID,
		Symbol:          "7203",
		Timestamp:       now,
		DecisionType:    domain.JevDecisionTypeScout,
		StateHash:       "abc123",
		StateJSON:       `{"price":2100}`,
		QuestionVersion: "scout-v1",
		ResponseJSON:    `{"interesting_now":0.8}`,
		LatencyMs:       420,
		ModelID:         "jev-scout-model",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if created.ID == 0 {
		t.Fatalf("expected assigned ID, got 0")
	}
	if created.CreatedAt.IsZero() {
		t.Fatalf("expected created_at to be populated, got %+v", created)
	}
	// Scout decisions carry no direction/confidence (er.md §jev_decisions:
	// "direction ... decision_type=trader時のみ設定").
	if created.Direction != nil || created.Confidence != nil {
		t.Fatalf("Insert() = %+v, want nil Direction/Confidence for a scout decision", created)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.DecisionType != domain.JevDecisionTypeScout || got.StateHash != "abc123" || got.QuestionVersion != "scout-v1" {
		t.Fatalf("Get(%d) = %+v, want DecisionType/StateHash/QuestionVersion to match Insert input", created.ID, got)
	}
	if got.ResponseJSON != `{"interesting_now":0.8}` || got.LatencyMs != 420 || got.ModelID != "jev-scout-model" {
		t.Fatalf("Get(%d) = %+v, want ResponseJSON/LatencyMs/ModelID to match Insert input", created.ID, got)
	}
	if !got.Timestamp.Equal(now) {
		t.Fatalf("Get(%d).Timestamp = %v, want %v", created.ID, got.Timestamp, now)
	}
}

func TestDecisionRepository_InsertAndGet_TraderDecisionWithDirectionAndConfidence(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	ctx := context.Background()
	confidence := 0.82
	cost := 0.0031

	created, err := repo.Insert(ctx, domain.JevDecision{
		InstrumentID:    instrumentID,
		Symbol:          "7203",
		Timestamp:       time.Date(2026, 9, 27, 9, 32, 0, 0, time.UTC),
		DecisionType:    domain.JevDecisionTypeTrader,
		StateHash:       "def456",
		StateJSON:       `{"price":2110}`,
		QuestionVersion: "trader-v1",
		ResponseJSON:    `{"direction":"LONG"}`,
		Direction:       ptr(domain.JevDirectionLong),
		Confidence:      &confidence,
		LatencyMs:       610,
		ModelID:         "jev-trader-model",
		RequestCost:     &cost,
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get(%d): %v", created.ID, err)
	}
	if got.Direction == nil || *got.Direction != domain.JevDirectionLong {
		t.Fatalf("Get(%d).Direction = %v, want %q", created.ID, got.Direction, domain.JevDirectionLong)
	}
	if got.Confidence == nil || *got.Confidence != confidence {
		t.Fatalf("Get(%d).Confidence = %v, want %v", created.ID, got.Confidence, confidence)
	}
	if got.RequestCost == nil || *got.RequestCost != cost {
		t.Fatalf("Get(%d).RequestCost = %v, want %v", created.ID, got.RequestCost, cost)
	}
}

func TestDecisionRepository_Get_NotFound(t *testing.T) {
	repo, _ := openTestDecisionRepo(t)

	_, err := repo.Get(context.Background(), 999999)
	if !errors.Is(err, repository.ErrDecisionNotFound) {
		t.Fatalf("Get(unknown) error = %v, want ErrDecisionNotFound", err)
	}
}

func TestDecisionRepository_ListByInstrument_MostRecentFirstAndRespectsLimit(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 27, 9, 30, 0, 0, time.UTC)

	for i, ts := range []time.Time{base, base.Add(1 * time.Minute), base.Add(2 * time.Minute)} {
		if _, err := repo.Insert(ctx, domain.JevDecision{
			InstrumentID:    instrumentID,
			Symbol:          "7203",
			Timestamp:       ts,
			DecisionType:    domain.JevDecisionTypeScout,
			StateHash:       "hash",
			StateJSON:       "{}",
			QuestionVersion: "scout-v1",
			ResponseJSON:    "{}",
			LatencyMs:       100 + i,
			ModelID:         "jev-scout-model",
		}); err != nil {
			t.Fatalf("Insert decision %d: %v", i, err)
		}
	}

	got, err := repo.ListByInstrument(ctx, instrumentID, 2)
	if err != nil {
		t.Fatalf("ListByInstrument: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListByInstrument() returned %d rows, want 2 (limit)", len(got))
	}
	if !got[0].Timestamp.Equal(base.Add(2 * time.Minute)) {
		t.Fatalf("ListByInstrument()[0].Timestamp = %v, want the most recent row first", got[0].Timestamp)
	}
	if !got[1].Timestamp.Equal(base.Add(1 * time.Minute)) {
		t.Fatalf("ListByInstrument()[1].Timestamp = %v, want the second most recent row", got[1].Timestamp)
	}
}

func ptr[T any](v T) *T { return &v }
