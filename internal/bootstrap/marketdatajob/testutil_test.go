package marketdatajob

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	configdefaults "github.com/ousiassllc/pitha-trador/config"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/jobqueue"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/trading"
	"github.com/ousiassllc/pitha-trador/internal/service/assist"
	"github.com/ousiassllc/pitha-trador/internal/service/execution"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
	"github.com/ousiassllc/pitha-trador/internal/service/newsfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
	"github.com/ousiassllc/pitha-trador/internal/service/screener"
)

// tradingHours is a fixed instant inside the 前場 (2026-09-29 10:00 JST): the
// session-gated Execution must not depend on the wall clock (issue #512).
var tradingHours = time.Date(2026, 9, 29, 10, 0, 0, 0, marketcalendar.JST)

// barClock is the clock tests give Handler and Execution: one minute after
// tradingHours, still inside the session.
func barClock() time.Time { return tradingHours.Add(time.Minute) }

// fakeBoards is the BoardSource stand-in: it returns board (or err) for
// every symbol.
type fakeBoards struct {
	board marketdata.Board
	err   error
}

func (f *fakeBoards) Latest(context.Context, string) (marketdata.Board, error) {
	return f.board, f.err
}

// testEnv is a Handler over a fresh DB, the compiled-in strategy.yaml/
// risk.yaml defaults and a fakeBoards source, plus the repositories tests
// assert through.
type testEnv struct {
	*Handler
	DB        *sql.DB
	Fake      *fakeBoards
	Jobs      *jobqueue.JobRepository
	Positions *trading.PositionRepository
}

// newTestEnv is newTestEnvWithNews with News Ingest unconfigured (its
// cache is always empty, so no news flag is ever raised).
func newTestEnv(t testing.TB) testEnv {
	t.Helper()
	return newTestEnvWithNews(t, newsfeed.FeedConfig{}, assist.Config{Label: "luna"})
}

func newTestEnvWithNews(t testing.TB, feedCfg newsfeed.FeedConfig, lunaCfg assist.Config) testEnv {
	t.Helper()
	conn, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	strategy, err := config.LoadStrategyBytes(configdefaults.DefaultStrategyYAML)
	if err != nil {
		t.Fatalf("LoadStrategyBytes: %v", err)
	}
	riskCfg, err := config.LoadRiskBytes(configdefaults.DefaultRiskYAML)
	if err != nil {
		t.Fatalf("LoadRiskBytes: %v", err)
	}

	instruments := market.NewInstrumentRepository(conn)
	snapshots := market.NewSnapshotRepository(conn)
	decisions := judgement.NewDecisionRepository(conn)
	jobs := jobqueue.NewJobRepository(conn)
	positions := trading.NewPositionRepository(conn)
	executionConfig := execution.ConfigFromRiskLimits(riskCfg.Paper)
	executionConfig.Calendar = marketcalendar.TSE
	executionConfig.Now = barClock
	boards := &fakeBoards{}
	return testEnv{
		Handler: &Handler{
			Boards:        boards,
			Instruments:   instruments,
			Snapshots:     snapshots,
			FeatureEngine: featureengine.NewEngine(snapshots, rag.NewService(conn, decisions, snapshots)),
			Execution: execution.NewEngine(execution.Deps{
				Orders: trading.NewOrderRepository(conn), Positions: positions, Snapshots: snapshots,
				Decisions: decisions, Signals: trading.NewSignalRepository(conn), Instruments: instruments,
			}, executionConfig),
			Screener:     screener.NewLiveSource(),
			News:         newsfeed.NewService(newsfeed.NewFeedClient(feedCfg), assist.NewLuna(assist.NewClient(lunaCfg)), instruments),
			Scheduler:    scheduler.New(jobs, instruments),
			EventTrigger: strategy.Scan.EventTrigger,
			Now:          barClock,
		},
		DB: conn, Fake: boards, Jobs: jobs, Positions: positions,
	}
}

func mustCreateInstrument(t *testing.T, env testEnv, symbol string) domain.Instrument {
	t.Helper()
	inst, err := env.Instruments.Create(context.Background(), domain.Instrument{
		Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: true,
	})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	return inst
}
