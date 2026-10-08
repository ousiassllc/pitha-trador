package runflow_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// With the 立花 adapter selected the saved watch list is the only daytime
// ingestion (issue #731): the 60秒 full REST scan is off even when
// scan.full_scan_enabled is true, so no pass over the whole universe is ever
// enqueued (#720). kabu keeps its own switch: its full scan still enqueues
// when it is asked for.
func TestBuildServices_TachibanaNeverRunsTheFullScan(t *testing.T) {
	yes := true
	inSession := time.Date(2026, 9, 29, 10, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	for name, tc := range map[string]struct {
		provider     string
		wantEnqueued int
	}{
		"tachibana": {config.BrokerTachibana, 0},
		"kabu":      {config.BrokerKabu, 1},
	} {
		t.Run(name, func(t *testing.T) {
			state := newState(t)
			state.Strategy.Scan.FullScanEnabled = &yes
			svc := bootstrap.BuildServices(state, config.Secrets{KabuAPIPassword: "pw", JevBaseURL: "http://127.0.0.1:1"},
				bootstrap.WithBrokerSettings(config.BrokerSettings{Provider: tc.provider}))
			if _, err := svc.Instruments.Create(context.Background(), domain.Instrument{
				Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true,
			}); err != nil {
				t.Fatalf("Create instrument: %v", err)
			}

			n, err := svc.Scheduler.EnqueueFullScan(context.Background(), inSession)
			if err != nil {
				t.Fatalf("EnqueueFullScan: %v", err)
			}
			if n != tc.wantEnqueued {
				t.Errorf("full scan enqueued %d jobs, want %d", n, tc.wantEnqueued)
			}
		})
	}
}
