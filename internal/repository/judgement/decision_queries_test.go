package judgement_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

func TestDecisionRepository_ListRecent_AcrossInstrumentsFiltersByType(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	insert := func(decisionType string, at time.Time) domain.JevDecision {
		t.Helper()
		d, err := repo.Insert(ctx, domain.JevDecision{
			InstrumentID: instrumentID, Symbol: "7203", Timestamp: at, DecisionType: decisionType,
			StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
		})
		if err != nil {
			t.Fatalf("Insert(%s): %v", decisionType, err)
		}
		return d
	}
	scout := insert(domain.JevDecisionTypeScout, base)
	trader := insert(domain.JevDecisionTypeTrader, base.Add(time.Minute))

	all, err := repo.ListRecent(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(all) != 2 || all[0].ID != trader.ID || all[1].ID != scout.ID {
		t.Fatalf("ListRecent(all) = %+v, want [trader scout]", all)
	}

	scouts, _ := repo.ListRecent(ctx, domain.JevDecisionTypeScout, 10)
	if len(scouts) != 1 || scouts[0].ID != scout.ID {
		t.Fatalf("ListRecent(scout) = %+v, want only the scout decision", scouts)
	}
}

func TestDecisionRepository_Observer_SeesInsertedRow(t *testing.T) {
	repo, instrumentID := openTestDecisionRepo(t)
	var seen []domain.JevDecision
	repo.SetObserver(func(_ context.Context, d domain.JevDecision) { seen = append(seen, d) })

	created, err := repo.Insert(context.Background(), domain.JevDecision{
		InstrumentID: instrumentID, Symbol: "7203", Timestamp: time.Now().UTC(), DecisionType: domain.JevDecisionTypeScout,
		StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
	})
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if len(seen) != 1 || seen[0].ID != created.ID || seen[0].Symbol != "7203" {
		t.Fatalf("observed = %+v, want the inserted row (ID %d)", seen, created.ID)
	}
}
