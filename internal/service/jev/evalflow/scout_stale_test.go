package evalflow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// Issue #685: HandleJob must not call Jev (billed) nor enqueue a
// jev-trader job on a bar older than domain.MaxSnapshotAge, and must
// return nil (a retry would see the same bar - no retry storm). A bar
// exactly MaxSnapshotAge old is still fresh.
func TestScout_HandleJob_SkipsStaleSnapshot(t *testing.T) {
	barAt := time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		age       time.Duration
		maxAge    time.Duration // 0 keeps the default (ranking-watch) domain.MaxSnapshotAge
		wantStale bool
	}{
		{"exactly max age is fresh", domain.MaxSnapshotAge, 0, false},
		{"just over max age is stale", domain.MaxSnapshotAge + time.Nanosecond, 0, true},
		{"previous session", 17 * time.Hour, 0, true},
		// Issue #686: a full-scan REST bar is only refreshed once per ~8 min
		// cycle, so the full-scan age (WithSnapshotMaxAge) lets it through.
		{"8 min bar is stale in ranking-watch mode", 8 * time.Minute, 0, true},
		{"8 min bar is fresh within the full-scan age", 8 * time.Minute, 620 * time.Second, false},
		{"just over the full-scan age is stale", 620*time.Second + time.Nanosecond, 620 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			passing := jevtest.ScoutHandler(jev.ScoutResponse{InterestingNow: 0.9, LiquidityOk: 0.9, AbnormalActivity: 0.9})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				passing(w, r)
			}))
			t.Cleanup(server.Close)

			db := newTestDB(t)
			snapshots := market.NewSnapshotRepository(db)
			jobs := jobqueue.NewJobRepository(db)
			inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203")
			if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
				InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: barAt,
				Price: 2100, Volume: 1000, Turnover: 2_000_000, RawDataJSON: `{}`,
			}); err != nil {
				t.Fatalf("insert snapshot: %v", err)
			}
			decisions := judgement.NewDecisionRepository(db)
			opts := []jev.Option{jev.WithClock(func() time.Time { return barAt.Add(tc.age) })}
			if tc.maxAge > 0 {
				opts = append(opts, jev.WithSnapshotMaxAge(tc.maxAge))
			}
			scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1}), decisions, snapshots, jobs,
				rag.NewService(db, decisions, snapshots), testThresholds(), opts...)

			payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
			job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueJevScout, string(payload), time.Now().UTC())
			if err != nil {
				t.Fatalf("enqueue: %v", err)
			}
			if err := scout.HandleJob(context.Background(), job); err != nil {
				t.Fatalf("HandleJob = %v, want nil (skip is not a failure)", err)
			}

			_, claimErr := jobs.ClaimNext(context.Background(), jobqueue.JobQueueJevTrader, time.Now().UTC())
			if tc.wantStale {
				if calls.Load() != 0 {
					t.Errorf("Jev called %d times on a stale bar, want 0", calls.Load())
				}
				if claimErr == nil {
					t.Error("jev-trader job enqueued for a stale bar")
				}
				return
			}
			if calls.Load() == 0 || claimErr != nil {
				t.Errorf("fresh bar: Jev calls = %d, ClaimNext(jev-trader) err = %v; want a call and a trader job", calls.Load(), claimErr)
			}
		})
	}
}
