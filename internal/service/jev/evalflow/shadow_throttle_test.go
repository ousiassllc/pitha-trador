package evalflow_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
)

// throttleAPI is a Jev API whose rate limit the test controls: while saturated it
// answers every request (live or candidate) with status (429 or 529); otherwise
// it serves like fakeJev. saturateAfterLive models the shared key: the first live
// request it serves uses up the budget, so everything after it is rejected until
// the test calls recover. It counts the requests by who sent them (a candidate
// request carries candidateMarker).
type throttleAPI struct {
	inner             *fakeJev
	status            int
	saturateAfterLive bool

	mu                          sync.Mutex
	saturated                   bool
	live, shadow                int
	liveRejected, shadowRejects int
}

func newThrottleAPI(status int, saturateAfterLive bool) *throttleAPI {
	return &throttleAPI{inner: &fakeJev{}, status: status, saturateAfterLive: saturateAfterLive}
}

func (a *throttleAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(body))
	isShadow := strings.Contains(string(body), candidateMarker)
	a.mu.Lock()
	if isShadow {
		a.shadow++
	} else {
		a.live++
	}
	rejected := a.saturated
	switch {
	case rejected && isShadow:
		a.shadowRejects++
	case rejected:
		a.liveRejected++
	case !isShadow && a.saturateAfterLive:
		a.saturated, a.saturateAfterLive = true, false
	}
	a.mu.Unlock()
	if rejected {
		w.WriteHeader(a.status)
		return
	}
	a.inner.ServeHTTP(w, r)
}

func (a *throttleAPI) setSaturated(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.saturated = v
}

func (a *throttleAPI) counts() (live, shadow int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.live, a.shadow
}

// retryingConfig is the production retry policy (4 attempts) with a short
// backoff, so a 429 is retried for real but the test stays fast.
var retryingConfig = jev.Config{MaxAttempts: 4, RetryBaseDelay: 5 * time.Millisecond}

func (e *shadowEnv) liveTrader() (time.Duration, error) {
	e.t.Helper()
	start := time.Now()
	_, _, err := e.client.Trader(context.Background(), jev.TraderRequest{State: jev.ScoutState{Symbol: "7203", Timestamp: e.now, Price: 2100}})
	return time.Since(start), err
}

func (e *shadowEnv) throttle() *jevshadowsettings.Throttle {
	e.t.Helper()
	return jevshadowsettings.Load(context.Background(), e.settings).Throttle
}

// Issue #883: with the shadow on and the API answering 429, the live
// jev.Client.Trader makes no more attempts and takes no longer than with the
// shadow off, and the shadow itself sends one request and no retries, then
// leaves the API alone.
func TestShadow_429_DoesNotWorsenTheLiveTraderCall(t *testing.T) {
	type outcome struct {
		liveAttempts, shadowRequests int
		liveDuration                 time.Duration
	}
	run := func(shadowOn bool) outcome {
		api := newThrottleAPI(http.StatusTooManyRequests, true)
		e := newShadowEnvOn(t, metalabelsettings.ModePaper, api, api.inner, retryingConfig, shadow.WithMinInterval(0))
		if shadowOn {
			e.enable()
		}
		e.evaluate() // live call served; the budget is now used up, so the shadow's call is answered 429
		e.shadow.Wait()
		_, shadowSent := api.counts()
		if _, ok := e.client.LastThrottle(); ok {
			t.Fatal("the shadow's own 429 was noted as a live throttle")
		}
		before, _ := api.counts()
		duration, err := e.liveTrader()
		if err == nil {
			t.Fatal("the live call succeeded on a saturated API")
		}
		after, _ := api.counts()
		return outcome{liveAttempts: after - before, shadowRequests: shadowSent, liveDuration: duration}
	}

	off, on := run(false), run(true)
	if off.liveAttempts != 4 || on.liveAttempts > off.liveAttempts {
		t.Errorf("live attempts: shadow off %d, shadow on %d, want 4 and no more", off.liveAttempts, on.liveAttempts)
	}
	if on.liveDuration > off.liveDuration+200*time.Millisecond {
		t.Errorf("live duration: shadow off %s, shadow on %s: the shadow slowed the live call", off.liveDuration, on.liveDuration)
	}
	if off.shadowRequests != 0 || on.shadowRequests != 1 {
		t.Errorf("shadow requests: off %d, on %d, want 0 and exactly 1 (no retry after a 429)", off.shadowRequests, on.shadowRequests)
	}
}

// The shadow stops for the cooldown after a 429 or 529 (whichever the API says), the
// stop and its reason are stored, a 429/529 is no failed call, and after the cooldown
// the shadow sends again; a second 429 stops it again.
func TestShadow_StopsForTheCooldownAfterA429Or529(t *testing.T) {
	for status, reason := range map[int]string{http.StatusTooManyRequests: "api_rate_limited", 529: "api_overloaded"} {
		api := newThrottleAPI(status, true)
		e := newShadowEnvOn(t, metalabelsettings.ModePaper, api, api.inner, retryingConfig, shadow.WithMinInterval(0))
		e.enable()

		e.evaluate()
		e.shadow.Wait()
		th := e.throttle()
		if th == nil || th.Status != status || th.Reason != reason || th.Source != jevshadowsettings.ThrottleSourceShadow || th.Count != 1 ||
			th.At != "2026-10-22T01:00:00Z" || th.Until != "2026-10-22T01:15:00Z" || !th.ActiveAt(e.now) {
			t.Fatalf("%d: throttle record = %+v, want a shadow-sourced %s stop of 15 minutes", status, th, reason)
		}
		cfg := jevshadowsettings.Load(context.Background(), e.settings)
		if cfg.Failures != nil || cfg.Halt != nil {
			t.Errorf("%d: a throttle counted as a failed call or a day stop: failures %+v halt %+v", status, cfg.Failures, cfg.Halt)
		}

		// The API recovers, but the shadow stays off until the cooldown is over.
		api.setSaturated(false)
		for _, at := range []time.Duration{0, 14*time.Minute + 59*time.Second} {
			e.now = time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC).Add(at)
			e.evaluate()
			e.shadow.Wait()
			if _, sent := api.counts(); sent != 1 {
				t.Fatalf("%d: the shadow sent a request %s into its cooldown (%d candidate requests)", status, at, sent)
			}
		}
		e.now = time.Date(2026, 10, 22, 1, 15, 0, 0, time.UTC)
		e.evaluate()
		e.shadow.Wait()
		if _, sent := api.counts(); sent != 2 || e.count(`SELECT COUNT(*) FROM jev_shadow_decisions`) != 1 {
			t.Fatalf("%d: after the cooldown: %d candidate requests, want 2 (one stored row)", status, sent)
		}

		// The API is throttling again: one more request, one more stop, counted.
		api.mu.Lock()
		api.saturateAfterLive = true
		api.mu.Unlock()
		e.now = e.now.Add(time.Minute)
		e.evaluate()
		e.shadow.Wait()
		if th := e.throttle(); th == nil || th.Count != 2 || !th.ActiveAt(e.now) {
			t.Errorf("%d: second stop = %+v, want count 2 and active", status, th)
		}
	}
}

// A 429/529 that a live call got stops the shadow too: its next comparison
// sends nothing, records the stop as live-sourced, and resumes after the cooldown.
func TestShadow_StopsWhenALiveCallWasThrottled(t *testing.T) {
	api := newThrottleAPI(http.StatusTooManyRequests, false)
	e := newShadowEnvOn(t, metalabelsettings.ModePaper, api, api.inner, retryingConfig, shadow.WithMinInterval(0))
	e.enable()

	api.setSaturated(true)
	if _, err := e.liveTrader(); err == nil {
		t.Fatal("the live call succeeded on a saturated API")
	}
	if ev, ok := e.client.LastThrottle(); !ok || ev.Status != 429 {
		t.Fatalf("LastThrottle = %+v, %v after a live 429", ev, ok)
	}
	api.setSaturated(false)

	e.evaluate()
	e.shadow.Wait()
	if _, sent := api.counts(); sent != 0 {
		t.Fatalf("the shadow sent %d requests right after a live 429", sent)
	}
	th := e.throttle()
	if th == nil || th.Source != jevshadowsettings.ThrottleSourceLive || th.Status != 429 || th.Count != 1 || !th.ActiveAt(e.now) {
		t.Fatalf("throttle record = %+v, want a live-sourced 429 stop", th)
	}
	// Seeing the same live 429 again does not count another event.
	e.evaluate()
	e.shadow.Wait()
	if th := e.throttle(); th.Count != 1 {
		t.Errorf("the same live 429 was counted %d times", th.Count)
	}

	e.now = e.now.Add(jevshadowsettings.ThrottleCooldown)
	e.evaluate()
	e.shadow.Wait()
	if _, sent := api.counts(); sent != 1 {
		t.Errorf("candidate requests after the cooldown = %d, want 1", sent)
	}
}

// The shadow sends at most one request per MinCallInterval; a comparison that
// would start sooner is dropped, and the live decision is made all the same.
func TestShadow_PacesItsCalls(t *testing.T) {
	api := newThrottleAPI(http.StatusTooManyRequests, false)
	clock := time.Date(2026, 10, 22, 1, 0, 0, 0, time.UTC)
	e := newShadowEnvOn(t, metalabelsettings.ModePaper, api, api.inner, jev.Config{MaxAttempts: 1},
		shadow.WithMinInterval(jevshadowsettings.MinCallInterval), shadow.WithPaceClock(func() time.Time { return clock }))
	e.enable()
	for range 3 {
		if live := e.evaluate(); live.ID == 0 {
			t.Fatal("the live decision stopped")
		}
		e.shadow.Wait()
	}
	if _, sent := api.counts(); sent != 1 {
		t.Fatalf("candidate requests within one interval = %d, want 1", sent)
	}
	clock = clock.Add(jevshadowsettings.MinCallInterval - time.Nanosecond)
	e.evaluate()
	e.shadow.Wait()
	if _, sent := api.counts(); sent != 1 {
		t.Fatalf("a request went out %s after the last (the interval is %s)", jevshadowsettings.MinCallInterval-time.Nanosecond, jevshadowsettings.MinCallInterval)
	}
	clock = clock.Add(time.Nanosecond)
	e.evaluate()
	e.shadow.Wait()
	if _, sent := api.counts(); sent != 2 {
		t.Fatalf("candidate requests a full interval after the last = %d, want 2", sent)
	}
}

// ShadowTrader never waits out a 429/529 (one request) but keeps the retries for
// other errors; Trader and Ask still retry and note a 429 (the live behaviour is
// unchanged).
func TestClient_ShadowTraderGivesUpOn429ButTraderRetries(t *testing.T) {
	api := newThrottleAPI(http.StatusTooManyRequests, false)
	e := newShadowEnvOn(t, metalabelsettings.ModePaper, api, api.inner, retryingConfig)
	api.setSaturated(true)
	req := jev.TraderRequest{QuestionVersion: "trader-v4", State: jev.ScoutState{Symbol: "7203", Timestamp: e.now, Price: 2100}}

	if _, _, err := e.client.ShadowTrader(context.Background(), req); err == nil {
		t.Fatal("ShadowTrader succeeded on a saturated API")
	}
	if _, sent := api.counts(); sent != 1 {
		t.Fatalf("ShadowTrader made %d requests on a 429, want 1", sent)
	}
	if _, ok := e.client.LastThrottle(); ok {
		t.Error("ShadowTrader's 429 was noted")
	}

	if _, err := e.liveTrader(); err == nil {
		t.Fatal("Trader succeeded on a saturated API")
	}
	if live, _ := api.counts(); live != 4 {
		t.Fatalf("Trader made %d requests on a 429, want 4 (the live retry policy)", live)
	}
	if ev, ok := e.client.LastThrottle(); !ok || ev.Status != 429 || !ev.At.Equal(e.now) {
		t.Errorf("LastThrottle after Trader = %+v, %v", ev, ok)
	}

	// Other retryable errors (a 503) are still retried by the shadow call.
	api2 := newThrottleAPI(http.StatusServiceUnavailable, false)
	e2 := newShadowEnvOn(t, metalabelsettings.ModePaper, api2, api2.inner, retryingConfig)
	api2.setSaturated(true)
	if _, _, err := e2.client.ShadowTrader(context.Background(), req); err == nil {
		t.Fatal("ShadowTrader succeeded on a failing API")
	}
	if _, sent := api2.counts(); sent != 4 {
		t.Errorf("ShadowTrader made %d requests on a 503, want 4 (unchanged retry policy)", sent)
	}
}
