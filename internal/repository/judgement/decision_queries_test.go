package judgement_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
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

func TestDecisionRepository_LatestTraderByInstruments_PicksNewestTraderPerInstrument(t *testing.T) {
	db := newTestDB(t)
	repo := judgement.NewDecisionRepository(db)
	a := insertInstrument(t, db, "1001", "A")
	b := insertInstrument(t, db, "1002", "B")
	c := insertInstrument(t, db, "1003", "C")
	d := insertInstrument(t, db, "1004", "D")
	ctx := context.Background()
	base := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)

	insert := func(instrumentID int64, decisionType string, at time.Time) domain.JevDecision {
		t.Helper()
		row, err := repo.Insert(ctx, domain.JevDecision{
			InstrumentID: instrumentID, Symbol: "S", Timestamp: at, DecisionType: decisionType,
			StateHash: "h", StateJSON: "{}", QuestionVersion: "v1", ResponseJSON: "{}", ModelID: "m",
		})
		if err != nil {
			t.Fatalf("Insert: %v", err)
		}
		return row
	}
	insert(a, domain.JevDecisionTypeTrader, base)
	newestA := insert(a, domain.JevDecisionTypeTrader, base.Add(time.Minute))
	insert(a, domain.JevDecisionTypeScout, base.Add(2*time.Minute)) // newer Scout must not win
	onlyB := insert(b, domain.JevDecisionTypeTrader, base)
	insert(c, domain.JevDecisionTypeScout, base)  // Scout only: absent
	insert(d, domain.JevDecisionTypeTrader, base) // not requested: absent

	got, err := repo.LatestTraderByInstruments(ctx, []int64{a, b, c})
	if err != nil {
		t.Fatalf("LatestTraderByInstruments: %v", err)
	}
	if len(got) != 2 || got[a].ID != newestA.ID || got[b].ID != onlyB.ID {
		t.Fatalf("LatestTraderByInstruments = %+v, want {a: newest trader, b: its trader} only", got)
	}

	empty, err := repo.LatestTraderByInstruments(ctx, nil)
	if err != nil || len(empty) != 0 {
		t.Fatalf("LatestTraderByInstruments(nil) = %v, %v, want empty map and nil error", empty, err)
	}
}
