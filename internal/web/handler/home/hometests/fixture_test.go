package hometests

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/activityfeed"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
)

// fixture is a Home screen whose every source is a fake; the zero state is a
// quiet trading morning (Tuesday 2026-10-20 10:00 JST, system running, a model
// adopted, nothing to attend to). A test changes the field it is about.
type fixture struct {
	now time.Time

	source    fakeSource
	state     domain.SystemState
	stateErr  error
	halt      *domain.DailyEntryHalt
	haltErr   error
	market    organisms.MarketDataBannerProps
	alerts    []domain.HealthAlert
	alertsErr error
	unsetKeys []string
	update    updater.Status
	cycle     *domain.ScanCycle
	scanErr   error
	settings  fakeSettings
	events    []domain.ActivityEvent
	eventsErr error
}

// jst is a JST instant of October 2026.
func jst(day, h, m int) time.Time { return time.Date(2026, 10, day, h, m, 0, 0, marketcalendar.JST) }

func newFixture() *fixture {
	return &fixture{
		now:   jst(20, 10, 0),
		state: domain.SystemStateRunning,
		source: fakeSource{health: home.Health{
			MetaAdopted: true, MetaVersion: "v1", MetaTrainedAt: jst(19, 9, 0), OpenedToday: 1,
		}},
		settings: fakeSettings{values: map[string]string{
			"policy.long.min_entry_quality":        "good",
			"policy.short.min_entry_quality":       "strong",
			"policy.paper.long.min_entry_quality":  "fair",
			"policy.paper.short.min_entry_quality": "good",
		}},
	}
}

func (f *fixture) engine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	deps := home.Deps{
		Source:     &f.source,
		State:      fakeState{f},
		DailyHalt:  fakeHalt{f},
		MarketData: fakeMarket{f},
		Alerts:     fakeAlerts{f},
		Secrets:    fakeSecrets{f},
		Update:     fakeUpdate{f},
		Settings:   &f.settings,
		Activity:   fakeActivity{f},
		Now:        func() time.Time { return f.now },
	}
	if f.cycle != nil || f.scanErr != nil {
		deps.Scan = fakeScan{f}
	}
	home.New(deps).Register(engine)
	return engine
}

// get serves path and returns the status and the body.
func (f *fixture) get(t *testing.T, path string) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	f.engine().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code, rec.Body.String()
}

// mustGet is get that requires 200.
func (f *fixture) mustGet(t *testing.T, path string) string {
	t.Helper()
	code, body := f.get(t, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, code)
	}
	return body
}

// sentence is the 取引の状態 card's one-line verdict.
func (f *fixture) sentence(t *testing.T) string {
	t.Helper()
	return inner(t, f.mustGet(t, "/home/trade-status"), "trade-status-sentence")
}

var (
	tagRe   = regexp.MustCompile(`<[^>]*>`)
	spaceRe = regexp.MustCompile(`\s+`)
)

// inner is the visible text of the first element carrying data-testid=id.
func inner(t *testing.T, body, id string) string {
	t.Helper()
	marker := `data-testid="` + id + `"`
	at := strings.Index(body, marker)
	if at < 0 {
		t.Fatalf("no element with %s in:\n%s", marker, body)
	}
	start := strings.LastIndex(body[:at], "<")
	name := regexp.MustCompile(`^<([a-z0-9]+)`).FindStringSubmatch(body[start:])[1]
	open := at + strings.Index(body[at:], ">") + 1
	end := open + strings.Index(body[open:], "</"+name+">")
	return clean(body[open:end])
}

// clean strips tags, decodes entities and collapses whitespace.
func clean(s string) string {
	return strings.TrimSpace(spaceRe.ReplaceAllString(html.UnescapeString(tagRe.ReplaceAllString(s, " ")), " "))
}

// count is how many elements carry data-testid=id.
func count(body, id string) int { return strings.Count(body, `data-testid="`+id+`"`) }

var errBoom = errors.New("boom")

type fakeSource struct {
	health      home.Health
	healthErr   error
	realized    home.Realized
	realizedErr error
	rejects     []home.RejectCount
	rejectsErr  error
	proposals   home.Proposals
	proposalErr error
	trades      []home.Trade
	tradesErr   error
	// positions are the rows of the positions table (open and closed);
	// OpenPositions returns the open ones, like the SELECT it stands for.
	positions []domain.Position
	posErr    error
	// healthCalls counts the Health reads, to tell a memoized one from a new one.
	healthCalls int
}

func (s *fakeSource) Health(context.Context, time.Time) (home.Health, error) {
	s.healthCalls++
	return s.health, s.healthErr
}

func (s *fakeSource) RealizedToday(context.Context, time.Time) (home.Realized, error) {
	return s.realized, s.realizedErr
}

func (s *fakeSource) RejectReasons(context.Context, time.Time) ([]home.RejectCount, error) {
	return s.rejects, s.rejectsErr
}

func (s *fakeSource) Proposals(context.Context, time.Time) (home.Proposals, error) {
	return s.proposals, s.proposalErr
}

func (s *fakeSource) RecentTrades(context.Context, time.Time, int) ([]home.Trade, error) {
	return s.trades, s.tradesErr
}

func (s *fakeSource) OpenPositions(context.Context) ([]domain.Position, error) {
	var open []domain.Position
	for _, p := range s.positions {
		if p.IsOpen() {
			open = append(open, p)
		}
	}
	return open, s.posErr
}

type fakeState struct{ f *fixture }

func (s fakeState) State(context.Context) (domain.SystemState, []domain.KillSwitchEvent, error) {
	return s.f.state, nil, s.f.stateErr
}

type fakeHalt struct{ f *fixture }

func (s fakeHalt) DailyHalt(context.Context) (*domain.DailyEntryHalt, error) {
	return s.f.halt, s.f.haltErr
}

type fakeMarket struct{ f *fixture }

func (s fakeMarket) BannerProps(context.Context) organisms.MarketDataBannerProps { return s.f.market }

type fakeAlerts struct{ f *fixture }

func (s fakeAlerts) Active(context.Context) ([]domain.HealthAlert, error) {
	return s.f.alerts, s.f.alertsErr
}

type fakeSecrets struct{ f *fixture }

func (s fakeSecrets) UnsetOptionalKeys(context.Context) []string { return s.f.unsetKeys }

type fakeUpdate struct{ f *fixture }

func (s fakeUpdate) Status() updater.Status { return s.f.update }

type fakeScan struct{ f *fixture }

func (s fakeScan) Scan(context.Context) (domain.ScanCycle, bool, error) {
	if s.f.cycle == nil {
		return domain.ScanCycle{}, false, s.f.scanErr
	}
	return *s.f.cycle, true, s.f.scanErr
}

type fakeSettings struct {
	values   map[string]string
	guard    dailyguardsettings.Resolved
	getErr   error
	guardErr error
}

func (s *fakeSettings) Get(_ context.Context, key string) (opsettings.Value, error) {
	return opsettings.Value{Key: key, Current: s.values[key]}, s.getErr
}

func (s *fakeSettings) DailyGuard(context.Context) (dailyguardsettings.Resolved, error) {
	return s.guard, s.guardErr
}

type fakeActivity struct{ f *fixture }

func (s fakeActivity) Snapshot(_ context.Context, q activityfeed.Query) (domain.ActivitySnapshot, error) {
	var events []domain.ActivityEvent
	for _, e := range s.f.events {
		if q.Type == "" || e.Type == q.Type {
			events = append(events, e)
		}
	}
	return domain.ActivitySnapshot{Events: events}, s.f.eventsErr
}
