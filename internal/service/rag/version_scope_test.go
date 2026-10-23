package rag_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	calrepo "github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Issue #881: the similar cases Jev is shown after a promotion come from the
// live prompt's cohort only. Seven indexed decisions sit at the same distance
// from the query: labeled and unlabeled Trader decisions of trader-v3 and
// trader-v4, and a Scout decision (never dropped by version). The names of the
// cases are told apart by direction and future_return.
func TestService_Context_LeavesOutForeignPromptVersions(t *testing.T) {
	db := newTestDB(t)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "9433")
	decisions := judgement.NewDecisionRepository(db)
	outcomes := calrepo.NewCalibrationRepository(db)
	in := rag.FeatureInput{Return1m: ptr(0.01), Return5m: ptr(0.02)}
	at := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	plain := rag.NewService(db, decisions, market.NewSnapshotRepository(db))

	type seeded struct {
		version, kind, direction string
		futureReturn             *float64
	}
	seed := []seeded{
		{"trader-v3", domain.JevDecisionTypeTrader, domain.JevDirectionLong, ptr(0.3)},
		{"trader-v3", domain.JevDecisionTypeTrader, domain.JevDirectionLong, nil},
		{"trader-v4", domain.JevDecisionTypeTrader, domain.JevDirectionShort, ptr(-0.3)},
		{"trader-v4", domain.JevDecisionTypeTrader, domain.JevDirectionShort, nil},
		{"scout-v3", domain.JevDecisionTypeScout, "", nil},
	}
	for i, s := range seed {
		d := domain.JevDecision{
			InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: at.Add(time.Duration(i) * time.Minute), DecisionType: s.kind,
			StateHash: s.version + string(rune('a'+i)), StateJSON: `{}`, QuestionVersion: s.version, ResponseJSON: `{}`, ModelID: "jev-test",
		}
		if s.kind == domain.JevDecisionTypeTrader {
			confidence := 0.8
			direction := s.direction
			d.Direction, d.Confidence = &direction, &confidence
		}
		saved, err := decisions.Insert(context.Background(), d)
		if err != nil {
			t.Fatalf("insert decision: %v", err)
		}
		if err := plain.IndexDecision(context.Background(), saved.ID, in); err != nil {
			t.Fatalf("IndexDecision: %v", err)
		}
		if s.futureReturn != nil {
			if _, err := outcomes.Insert(context.Background(), domain.CalibrationOutcome{
				JevDecisionID: saved.ID, HorizonMinutes: 5, FutureReturn: *s.futureReturn, WasDirectionCorrect: boolPtr(true),
			}); err != nil {
				t.Fatalf("insert outcome: %v", err)
			}
		}
	}

	// kinds is "<direction>/<labeled>" per case, sorted; "scout" for a Scout case.
	kinds := func(t *testing.T, svc *rag.Service) []string {
		t.Helper()
		got, err := svc.Context(context.Background(), in, rag.Subject{}, 5)
		if err != nil {
			t.Fatalf("Context: %v", err)
		}
		var out []string
		for _, c := range got.Cases {
			switch {
			case c.Direction == nil:
				out = append(out, "scout")
			case c.FutureReturn != nil:
				out = append(out, *c.Direction+"/labeled")
			default:
				out = append(out, *c.Direction+"/unlabeled")
			}
		}
		slices.Sort(out)
		return out
	}
	scoped := func(foreign ...string) *rag.Service {
		return rag.NewService(db, decisions, market.NewSnapshotRepository(db),
			rag.WithForeignTraderVersions(func(context.Context) ([]string, error) { return foreign, nil }))
	}

	t.Run("no scope shows every version", func(t *testing.T) {
		want := []string{"LONG/labeled", "LONG/unlabeled", "SHORT/labeled", "SHORT/unlabeled", "scout"}
		if got := kinds(t, plain); !slices.Equal(got, want) {
			t.Fatalf("cases = %v, want %v", got, want)
		}
	})
	t.Run("live trader-v3 never shows trader-v4", func(t *testing.T) {
		want := []string{"LONG/labeled", "LONG/unlabeled", "scout"}
		if got := kinds(t, scoped("trader-v4")); !slices.Equal(got, want) {
			t.Fatalf("cases = %v, want %v", got, want)
		}
	})
	t.Run("live trader-v4 (after a promotion) never shows trader-v3", func(t *testing.T) {
		want := []string{"SHORT/labeled", "SHORT/unlabeled", "scout"}
		if got := kinds(t, scoped("trader-v1", "trader-v2", "trader-v3")); !slices.Equal(got, want) {
			t.Fatalf("cases = %v, want %v", got, want)
		}
	})
	t.Run("an unreadable scope is an error, not a mixed context", func(t *testing.T) {
		svc := rag.NewService(db, decisions, market.NewSnapshotRepository(db),
			rag.WithForeignTraderVersions(func(context.Context) ([]string, error) { return nil, errors.New("settings unreadable") }))
		if got, err := svc.Context(context.Background(), in, rag.Subject{}, 5); err == nil {
			t.Fatalf("Context = %+v, nil with an unreadable scope, want an error", got)
		}
	})
}
