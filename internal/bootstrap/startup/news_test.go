package startup_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/paperexec"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/jevtest"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// inSession is inside the 前場 so News Ingest's 東証立会時間 gate is open.
var inSession = time.Date(2026, 9, 29, 10, 0, 0, 0, marketcalendar.JST)

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs routes slog's default logger into the returned buffer until the
// test ends.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	sink := &syncBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(sink, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return sink
}

// buildNews builds Services on a fresh DB with the news clock pinned inside
// the session; nothing dials a real host (callers pass fake URLs).
func buildNews(t *testing.T, secrets config.Secrets, opts ...bootstrap.BuildOption) *bootstrap.Services {
	t.Helper()
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	secrets.KabuAPIPassword = "test-password"
	opts = append(opts, bootstrap.WithJevMaxAttempts(1),
		bootstrap.WithNewsClock(func() time.Time { return inSession.Add(time.Minute) }),
		bootstrap.WithExecutionClock(func() time.Time { return inSession.Add(time.Minute) }))
	return bootstrap.BuildServices(state, secrets, opts...)
}

// openPaperPosition creates instrument symbol and enters a paper LONG on it,
// so it is a held symbol News Ingest polls.
func openPaperPosition(t *testing.T, svc *bootstrap.Services, symbol string, price float64) domain.Position {
	t.Helper()
	ctx := context.Background()
	inst, err := svc.Instruments.Create(ctx, domain.Instrument{Symbol: symbol, Name: symbol + " Inc.", Market: "TSE Prime", IsActive: true})
	if err != nil {
		t.Fatalf("Create instrument %q: %v", symbol, err)
	}
	signal := domain.TradeSignal{InstrumentID: inst.ID, Symbol: symbol, Direction: domain.JevDirectionLong, RiskPassed: true, PolicyVersion: "v1"}
	snap := domain.Snapshot{InstrumentID: inst.ID, Symbol: symbol, Price: price, Timestamp: inSession}
	if err := (paperexec.Executor{Engine: svc.Execution, Sizer: svc.Risk}).ExecuteSignal(ctx, signal, snap); err != nil {
		t.Fatalf("ExecuteSignal %q: %v", symbol, err)
	}
	position, err := svc.Positions.GetOpenByInstrument(ctx, inst.ID)
	if err != nil {
		t.Fatalf("GetOpenByInstrument %q: %v", symbol, err)
	}
	return position
}

// Issue #273: the Jev key alone enables News Ingest (Luna on Jev, やのしん
// feed, no other key); without it - or with the feed switched off - the
// service still builds and simply does not poll.
func TestBuildServices_NewsIngestDefaultsOnWithJevKeyOnly(t *testing.T) {
	const dead = "http://127.0.0.1:1"
	for _, tc := range []struct {
		name    string
		secrets config.Secrets
		want    bool
	}{
		{"jev key only", config.Secrets{JevAPIKey: "k", JevBaseURL: dead}, true},
		{"no jev key", config.Secrets{JevBaseURL: dead}, false},
		{"feed switched off", config.Secrets{JevAPIKey: "k", JevBaseURL: dead, NewsFeedEnabled: "off"}, false},
		{"feed explicitly on", config.Secrets{JevAPIKey: "k", JevBaseURL: dead, NewsFeedEnabled: "on"}, true},
		{"luna override without jev key", config.Secrets{LunaBaseURL: dead}, true},
		{"custom feed url", config.Secrets{JevAPIKey: "k", JevBaseURL: dead, NewsFeedURL: dead + "/feed"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := buildNews(t, tc.secrets)
			if svc.News == nil {
				t.Fatal("Services.News is nil: it must exist even when its loop is not started")
			}
			if got := svc.NewsIngestEnabled(); got != tc.want {
				t.Errorf("NewsIngestEnabled = %v, want %v", got, tc.want)
			}
		})
	}
}

// NEWS_FEED_URL replaces the default feed with the generic contract.
func TestBuildServices_NewsFeedURLOverridesYanoshin(t *testing.T) {
	var generic, yanoshin atomic.Int32
	genericServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		generic.Add(1)
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	t.Cleanup(genericServer.Close)
	yanoshinServer := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { yanoshin.Add(1) }))
	t.Cleanup(yanoshinServer.Close)

	svc := buildNews(t, config.Secrets{JevAPIKey: "k", JevBaseURL: "http://127.0.0.1:1", NewsFeedURL: genericServer.URL},
		bootstrap.WithYanoshinBaseURL(yanoshinServer.URL))
	openPaperPosition(t, svc, "7203", 2500)
	if err := svc.News.Poll(context.Background()); err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if generic.Load() != 1 || yanoshin.Load() != 0 {
		t.Errorf("requests generic=%d yanoshin=%d, want the custom feed only", generic.Load(), yanoshin.Load())
	}
}

// Issue #273 fail-safe: with やのしん down, a poll raises no news flag, logs
// the error (log + Activity Feed), backs off instead of querying every
// cycle, and the rest of the system - Jev Scout and paper order flow - keeps
// working.
func TestBuildServices_YanoshinFailureRaisesNoFlagLogsErrorAndOtherFlowsContinue(t *testing.T) {
	var yanoshinHits atomic.Int32
	yanoshin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		yanoshinHits.Add(1)
		http.Error(w, "maintenance", http.StatusServiceUnavailable)
	}))
	t.Cleanup(yanoshin.Close)
	jevServer := httptest.NewServer(jevtest.ScoutHandler(jev.ScoutResponse{InterestingNow: 0.9}))
	t.Cleanup(jevServer.Close)
	logs := captureLogs(t)

	svc := buildNews(t, config.Secrets{JevAPIKey: "k", JevBaseURL: jevServer.URL}, bootstrap.WithYanoshinBaseURL(yanoshin.URL))
	if !svc.NewsIngestEnabled() {
		t.Fatal("news ingest is not enabled with a Jev key; the test would prove nothing")
	}
	openPaperPosition(t, svc, "7203", 2500)

	ctx := context.Background()
	if err := svc.News.Poll(ctx); err != nil {
		t.Fatalf("Poll with やのしん down returned %v, want nil", err)
	}
	if yanoshinHits.Load() != 1 {
		t.Fatalf("やのしん requests = %d, want 1 (one held symbol)", yanoshinHits.Load())
	}
	if svc.News.TakeNewsFlag("7203") {
		t.Error("news flag raised although the feed failed")
	}
	if _, ok := svc.News.NewsContext("7203"); ok {
		t.Error("news context cached although the feed failed")
	}
	out := logs.String()
	if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "newsfeed: fetch failed") || !strings.Contains(out, "status 503") {
		t.Errorf("log lacks the feed error; log=%s", out)
	}
	if !strings.Contains(out, "lowering the query frequency") {
		t.Errorf("log lacks the backoff notice; log=%s", out)
	}
	snap, err := svc.Activity.Snapshot(ctx, activityfeed.Query{Type: domain.ActivityTypeNewsFeed})
	if err != nil || len(snap.Events) != 1 || snap.Events[0].Symbol != "7203" || !strings.Contains(snap.Events[0].Detail, "503") {
		t.Errorf("Activity Feed news_feed events = %+v, %v, want the feed error for 7203", snap.Events, err)
	}

	// Backoff: the next cycle sends no request at all.
	if err := svc.News.Poll(ctx); err != nil || yanoshinHits.Load() != 1 {
		t.Errorf("second Poll: err=%v hits=%d, want nil and still 1 request (backing off)", err, yanoshinHits.Load())
	}

	// Jev Scout and the paper order flow are unaffected.
	scout, _, err := svc.Jev.Scout(ctx, jev.ScoutRequest{})
	if err != nil || scout.InterestingNow != 0.9 {
		t.Errorf("Jev.Scout = (%+v, %v), want the Jev answer", scout, err)
	}
	if position := openPaperPosition(t, svc, "6758", 1500); position.Side != domain.PositionSideLong {
		t.Errorf("paper entry after the feed failure = %+v, want a LONG position", position)
	}
}

// Issue #273: やのしん is unofficial, so start-up and shutdown must not depend
// on it. With only a Jev key (News Ingest on by default) and the feed
// answering 503, Start succeeds and Stop returns in bounded time.
func TestServices_StartAndStopToleratesYanoshinDown(t *testing.T) {
	yanoshin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(yanoshin.Close)
	kabu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(kabu.Close)

	svc := buildNews(t, config.Secrets{JevAPIKey: "jev-key", JevBaseURL: "http://127.0.0.1:1"},
		bootstrap.WithYanoshinBaseURL(yanoshin.URL), bootstrap.WithKabuBaseURL(kabu.URL+"/kabusapi"))
	if !svc.NewsIngestEnabled() {
		t.Fatal("news ingest is not enabled; the test would prove nothing")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start with やのしん down: err = %v, want nil", err)
	}
	cancel()
	stopped := make(chan struct{})
	go func() { svc.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("Stop did not return within 10s")
	}
}
