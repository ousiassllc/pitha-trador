package policy_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Issue #685: HandleJob must neither run Jev Trader / persist a signal /
// hand anything to the executor on a bar older than
// domain.MaxSnapshotAge, nor fail (no retry storm). A bar exactly that
// old is still fresh. The fixture bar is stamped 2026-09-27 09:31 UTC.
func TestHandler_HandleJob_SkipsStaleSnapshot(t *testing.T) {
	barAt := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		age       time.Duration
		wantStale bool
	}{
		{"exactly max age is fresh", domain.MaxSnapshotAge, false},
		{"just over max age is stale", domain.MaxSnapshotAge + time.Nanosecond, true},
		{"previous session", 17 * time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newHandlerTestDB(t)
			decisions := judgement.NewDecisionRepository(db)
			snapshots := market.NewSnapshotRepository(db)
			signals := trading.NewSignalRepository(db)
			client := jev.NewClient(jev.Config{BaseURL: traderServer(t, passingTraderResponse(domain.JevDirectionLong)).URL, MaxAttempts: 1})
			trader := jev.NewTrader(client, decisions, rag.NewService(db, decisions, snapshots))
			executor := &recordingExecutor{}
			handler := policy.NewHandler(trader, snapshots, policy.NewEngine(testThresholds(), nil, signals), executor,
				policy.WithClock(func() time.Time { return barAt.Add(tc.age) }))
			inst := mustCreateInstrumentAndSnapshot(t, db, "7203", 10)

			payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: "7203"})
			if err := handler.HandleJob(context.Background(), jobqueue.Job{PayloadJSON: string(payload)}); err != nil {
				t.Fatalf("HandleJob = %v, want nil (skip is not a failure)", err)
			}

			got, err := signals.ListByInstrument(context.Background(), inst.ID, 10)
			if err != nil {
				t.Fatalf("ListByInstrument: %v", err)
			}
			wantRows := 1
			if tc.wantStale {
				wantRows = 0
			}
			if len(got) != wantRows || len(executor.executed) != wantRows {
				t.Errorf("signals = %d, executed = %d, want %d each", len(got), len(executor.executed), wantRows)
			}
		})
	}
}
