package replayflow_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevreplaysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/jevreplay"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	service "github.com/ousiassllc/pitha-trador/internal/service/jevreplay"
)

// countingServer is an OpenRouter stand-in that counts every request it gets.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32, *[]string) {
	t.Helper()
	var n atomic.Int32
	markets := &[]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			State struct {
				Market json.RawMessage `json:"market"`
			} `json:"state"`
		}
		_ = json.Unmarshal(raw, &body)
		*markets = append(*markets, string(body.State.Market))
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	return server, &n, markets
}

// Completion criterion: with the switch off (the default) the runner makes no
// external request, whatever else is configured. Counted at the HTTP server with
// a real trialwire client, and at the runner's client factory.
func TestRunner_DefaultOffMakesNoExternalCall(t *testing.T) {
	for name, value := range map[string]string{"absent": "", "zero": "0", "damaged": "banana"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			server, requests, _ := countingServer(t)
			e.configure(0.000005, arm("a", ""))
			e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), state(1))
			run, _ := e.start(wholeOctober(10))
			if value != "" {
				e.set(jevreplaysettings.KeyEnabled, value)
			}
			client := trialwire.New(trialwire.Config{BaseURL: server.URL, APIKey: "test-key-xxxx", Model: "vendor/a"})
			factoryCalls := 0
			r := service.New(e.store, e.trial, e.settings, func(jevtrialsettings.Arm) service.Caller { factoryCalls++; return client },
				metalabelsettings.ModePaper, func() time.Time { return now }, service.WithMinInterval(0))
			t.Cleanup(r.Close)

			tick(t, r)
			r.Start()
			tick(t, r)

			if n := requests.Load(); n != 0 || factoryCalls != 0 {
				t.Errorf("%d requests and %d client lookups with the switch %s, want 0 and 0", n, factoryCalls, name)
			}
			if got := e.run(run.ID); got.Status != jevreplay.StatusPending || e.count("jev_replay_results") != 0 {
				t.Errorf("run = %s with %d results, want it untouched", got.Status, e.count("jev_replay_results"))
			}
		})
	}
}

// Without a key no request is made and the run ends as failed (key_not_set);
// likewise a run whose arm is no longer a registered prompt version.
func TestRunner_NoKeyMakesNoExternalCallAndFailsTheRun(t *testing.T) {
	e := newEnv(t)
	e.enable()
	server, requests, _ := countingServer(t)
	e.configure(0.000005, arm("a", ""))
	e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), state(1))
	run, _ := e.start(wholeOctober(10))
	noKey := trialwire.New(trialwire.Config{BaseURL: server.URL, Model: "vendor/a"}) // no API key
	r := service.New(e.store, e.trial, e.settings, func(jevtrialsettings.Arm) service.Caller { return noKey },
		metalabelsettings.ModePaper, func() time.Time { return now }, service.WithMinInterval(0))
	t.Cleanup(r.Close)

	tick(t, r)

	if n := requests.Load(); n != 0 {
		t.Errorf("%d requests without a key, want 0", n)
	}
	if got := e.run(run.ID); got.Status != jevreplay.StatusFailed || got.StopReason != jevreplay.StopKeyNotSet {
		t.Errorf("run = %s (%s), want failed with key_not_set", got.Status, got.StopReason)
	}
}

// Through the real wire client the arm receives the saved input as stored.
func TestRunner_TheWireRequestCarriesTheStoredInputAsIs(t *testing.T) {
	e := newEnv(t)
	e.enable()
	server, requests, markets := countingServer(t)
	e.configure(0.000005, arm("a", ""))
	want := state(7)
	e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), want)
	e.start(wholeOctober(10))
	client := trialwire.New(trialwire.Config{BaseURL: server.URL, APIKey: "test-key-xxxx", Model: "vendor/a"})
	r := service.New(e.store, e.trial, e.settings, func(jevtrialsettings.Arm) service.Caller { return client },
		metalabelsettings.ModePaper, func() time.Time { return now }, service.WithMinInterval(0), service.WithSecrets("test-key-xxxx"))
	t.Cleanup(r.Close)

	tick(t, r)

	if requests.Load() != 1 || len(*markets) != 1 || (*markets)[0] != want {
		t.Errorf("requests %d, market %v, want 1 request carrying %s", requests.Load(), *markets, want)
	}
	// The server answered 500: a failed row is recorded, counted at the worst case, with no key in it.
	var status, code, excerpt string
	if err := e.db.QueryRow(`SELECT status, failure_code, error_excerpt FROM jev_replay_results`).Scan(&status, &code, &excerpt); err != nil {
		t.Fatal(err)
	}
	if status != jevtrial.StatusFailed || code != jevtrial.FailureHTTP5xx || strings.Contains(excerpt, "test-key-xxxx") {
		t.Errorf("row = %s %s %q", status, code, excerpt)
	}
}

// The stop flag ends the run before any further call.
func TestRunner_StopFlagEndsTheRunWithoutCalls(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""))
	e.decide(time.Date(2026, 10, 5, 1, 0, 0, 0, time.UTC), state(1))
	run, _ := e.start(wholeOctober(10))
	if ok, err := repo.RequestStop(context.Background(), e.db, run.ID, now); err != nil || !ok {
		t.Fatalf("RequestStop = %v, %v", ok, err)
	}

	tick(t, e.runner())

	got := e.run(run.ID)
	if got.Status != jevreplay.StatusStopped || got.StopReason != jevreplay.StopRequested || !got.StopRequested || e.arms["a"].calls() != 0 {
		t.Errorf("run = %s (%s) after %d calls, want stopped (requested) with 0 calls", got.Status, got.StopReason, e.arms["a"].calls())
	}
	if ok, err := repo.RequestStop(context.Background(), e.db, run.ID, now); err != nil || ok {
		t.Errorf("a finished run accepted another stop: %v, %v", ok, err)
	}
}

// A run cut off by a shutdown stays running and, when started again, asks only the
// calls that have no row: nothing is asked twice.
func TestRunner_ACutOffRunResumesWithoutRepeatingRecordedCalls(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""))
	for i := range 3 {
		e.decide(time.Date(2026, 10, 5+i, 1, 0, 0, 0, time.UTC), state(i))
	}
	run, _ := e.start(wholeOctober(10))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	e.arms["a"].fn = func(c context.Context, n int) (trialwire.Response, error) {
		if n == 1 {
			return okResp(0.000005), nil
		}
		cancel() // the application shuts down during the second call
		<-c.Done()
		return trialwire.Response{}, c.Err()
	}

	if err := e.runner().Tick(ctx); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	got := e.run(run.ID)
	if got.Status != jevreplay.StatusRunning || got.CallsDone != 1 || e.count("jev_replay_results") != 1 {
		t.Fatalf("after the cut-off: run %s with %d done and %d rows, want running with 1", got.Status, got.CallsDone, e.count("jev_replay_results"))
	}

	e.arms["a"].fn = answer(0.000005)
	before := e.arms["a"].calls()
	tick(t, e.runner())

	if asked := e.arms["a"].calls() - before; asked != 2 {
		t.Errorf("the resumed run asked %d calls, want only the 2 without a row", asked)
	}
	if got := e.run(run.ID); got.Status != jevreplay.StatusCompleted || got.CallsDone != 3 || e.count("jev_replay_results") != 3 {
		t.Errorf("after the resume: run %s with %d done and %d rows, want completed with 3", got.Status, got.CallsDone, e.count("jev_replay_results"))
	}
}

// HTTP 402 (the shared key is out of credit) ends the run; the failed row is
// kept, the API key is redacted from its excerpt and no further call is made.
func TestRunner_PaymentRequiredEndsTheRunAndRedactsTheKey(t *testing.T) {
	e := newEnv(t)
	e.enable()
	e.configure(0.000005, arm("a", ""))
	for i := range 3 {
		e.decide(time.Date(2026, 10, 5+i, 1, 0, 0, 0, time.UTC), state(i))
	}
	run, _ := e.start(wholeOctober(10))
	e.arms["a"].fn = func(context.Context, int) (trialwire.Response, error) {
		return trialwire.Response{}, &jev.APIError{StatusCode: http.StatusPaymentRequired, Body: "out of credit for key test-key-xxxx"}
	}

	tick(t, e.runner(service.WithSecrets("test-key-xxxx")))

	got := e.run(run.ID)
	if got.Status != jevreplay.StatusStopped || got.StopReason != jevreplay.StopPaymentRequired {
		t.Errorf("run = %s (%s), want stopped by payment_required", got.Status, got.StopReason)
	}
	var excerpt string
	if err := e.db.QueryRow(`SELECT error_excerpt FROM jev_replay_results WHERE failure_code = 'http_402' LIMIT 1`).Scan(&excerpt); err != nil {
		t.Fatalf("no http_402 row: %v", err)
	}
	if strings.Contains(excerpt, "test-key-xxxx") || !strings.Contains(excerpt, "[redacted]") {
		t.Errorf("excerpt = %q, want the key redacted", excerpt)
	}
	if got.CallsFailed != got.CallsDone || got.CallsDone < 1 {
		t.Errorf("failed %d of %d done, want every recorded call failed", got.CallsFailed, got.CallsDone)
	}
}

// panicStore panics in NextActive until it has been called panics times, then
// answers like the real store.
type panicStore struct {
	*repo.Repository
	calls  atomic.Int32
	panics int32
	resume chan struct{}
}

func (s *panicStore) NextActive(ctx context.Context) (jevreplay.Run, bool, error) {
	n := s.calls.Add(1)
	if n <= s.panics {
		panic("replay store bug")
	}
	if n == s.panics+1 {
		close(s.resume)
	}
	return s.Repository.NextActive(ctx)
}

// A panic in one cycle of the resident loop is logged and the next poll runs
// the cycle again (FR-SCHED-6); it does not take the process down.
func TestRunner_StartSurvivesAPanicInACycle(t *testing.T) {
	e := newEnv(t)
	e.enable()
	store := &panicStore{Repository: e.store, panics: 1, resume: make(chan struct{})}
	r := service.New(store, e.trial, e.settings, func(jevtrialsettings.Arm) service.Caller { return nil },
		metalabelsettings.ModePaper, func() time.Time { return now }, service.WithMinInterval(0), service.WithPollInterval(5*time.Millisecond))
	t.Cleanup(r.Close)

	r.Start()

	select {
	case <-store.resume: // NextActive was called again after it panicked
	case <-time.After(10 * time.Second):
		t.Fatalf("NextActive called %d times, want the loop to run another cycle after the panic", store.calls.Load())
	}
}
