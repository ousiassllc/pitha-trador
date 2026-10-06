package evalflow_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

type recordedScout struct {
	symbol  string
	outcome domain.ScoutOutcome
}

type scoutRecorderStub struct{ got []recordedScout }

func (s *scoutRecorderStub) RecordScout(symbol string, outcome domain.ScoutOutcome) {
	s.got = append(s.got, recordedScout{symbol, outcome})
}

// runScoutJob handles one jev-scout job against server and returns the
// recorder's view and HandleJob's error (issue #303: Scanner funnel's
// Scout stage).
func runScoutJob(t *testing.T, server *httptest.Server) ([]recordedScout, error) {
	t.Helper()
	db := newTestDB(t)
	snapshots := market.NewSnapshotRepository(db)
	jobs := jobqueue.NewJobRepository(db)
	inst := mustCreateInstrument(t, market.NewInstrumentRepository(db), "7203")
	if _, err := snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: inst.ID, Symbol: inst.Symbol, Timestamp: time.Date(2026, 9, 27, 9, 31, 0, 0, time.UTC),
		Price: 2100, Volume: 1000, Turnover: 2_000_000, Feature: domain.Feature{VWAP: 2090}, RawDataJSON: `{}`,
	}); err != nil {
		t.Fatalf("insert snapshot: %v", err)
	}
	rec := &scoutRecorderStub{}
	decisions := judgement.NewDecisionRepository(db)
	scout := jev.NewScout(jev.NewClient(jev.Config{BaseURL: server.URL, MaxAttempts: 1}), decisions, snapshots, jobs,
		rag.NewService(db, decisions, snapshots), testThresholds(), jev.WithScoutRecorder(rec))

	payload, _ := json.Marshal(jev.ScoutJobPayload{InstrumentID: inst.ID, Symbol: inst.Symbol})
	job, err := jobs.Enqueue(context.Background(), jobqueue.JobQueueJevScout, string(payload), time.Now().UTC())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	return rec.got, scout.HandleJob(context.Background(), job)
}

func TestScout_HandleJob_ReportsVerdictToRecorder(t *testing.T) {
	tests := []struct {
		name string
		resp jev.ScoutResponse
		want domain.ScoutOutcome
	}{
		{"pass", jev.ScoutResponse{InterestingNow: 0.9, LiquidityOk: 0.9, AbnormalActivity: 0.9}, domain.ScoutPassed},
		{"fail", jev.ScoutResponse{InterestingNow: 0.1, LiquidityOk: 0.1, AbnormalActivity: 0.1}, domain.ScoutFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := runScoutJob(t, scoutServer(t, tt.resp))
			if err != nil {
				t.Fatalf("HandleJob: %v", err)
			}
			if len(got) != 1 || got[0] != (recordedScout{"7203", tt.want}) {
				t.Fatalf("recorded = %+v, want [{7203 %s}]", got, tt.want)
			}
		})
	}
}

func TestScout_HandleJob_ReportsJevFailureAsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) }))
	defer server.Close()
	got, err := runScoutJob(t, server)
	if err == nil {
		t.Fatal("HandleJob: want the Jev failure returned for retry")
	}
	if len(got) != 1 || got[0] != (recordedScout{"7203", domain.ScoutError}) {
		t.Fatalf("recorded = %+v, want [{7203 error}]", got)
	}
}
