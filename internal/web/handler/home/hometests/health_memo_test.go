package hometests

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// serve GETs path on one shared engine (one handler, so one health memo).
func serve(t *testing.T, engine http.Handler, path string) {
	t.Helper()
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, rec.Code)
	}
}

// The fragments of one page refresh together; they share one health_today read
// instead of running its queries once per fragment.
func TestHealth_FragmentsShareOneReadWithinAFewSeconds(t *testing.T) {
	f := newFixture()
	engine := f.engine()
	for _, path := range []string{"/home/trade-status", "/home/pnl", "/home/attention", "/home/details", "/home/attention-badge"} {
		serve(t, engine, path)
	}
	if f.source.healthCalls != 1 {
		t.Errorf("Health read %d times for 5 fragments in the same instant, want 1", f.source.healthCalls)
	}

	f.now = f.now.Add(10 * time.Second)
	serve(t, engine, "/home/attention")
	if f.source.healthCalls != 2 {
		t.Errorf("Health read %d times after the memo expired, want 2", f.source.healthCalls)
	}
}

// A change of the live state a fragment has just read (here the Kill Switch
// pausing, the moment the page refreshes) never serves it a health read taken
// under the old state.
func TestHealth_StateChangeBypassesTheMemo(t *testing.T) {
	f := newFixture()
	engine := f.engine()
	serve(t, engine, "/home/attention")
	f.state = domain.SystemStateKilled
	serve(t, engine, "/home/attention")
	if f.source.healthCalls != 2 {
		t.Errorf("Health read %d times across a state change, want 2", f.source.healthCalls)
	}
}

// A failed read is not kept: the next fragment retries.
func TestHealth_FailedReadIsNotMemoized(t *testing.T) {
	f := newFixture()
	f.source.healthErr = errBoom
	engine := f.engine()
	serve(t, engine, "/home/attention")
	serve(t, engine, "/home/trade-status")
	if f.source.healthCalls != 2 {
		t.Errorf("Health read %d times after a failure, want 2", f.source.healthCalls)
	}
}
