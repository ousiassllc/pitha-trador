package gmo

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a virtual clock: Sleep advances time instantly, so the
// 1-call-per-second spacing and the retry backoff cost no real time.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, time.January, 5, 9, 0, 0, 0, time.UTC)}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps = append(f.sleeps, d)
	f.now = f.now.Add(d)
	return nil
}

func (f *fakeClock) recordedSleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

// fixture reads a recorded GMO response from testdata.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// step is one scripted server response: the HTTP status (200 when zero) and
// a body, taken from a fixture file or given inline.
type step struct {
	status  int
	fixture string
	body    string
}

// fakeServer plays steps in order, one per request, and records each request
// URL and the fake-clock time it arrived at. A request beyond the script
// fails the test and gets a 500.
type fakeServer struct {
	*httptest.Server
	mu    sync.Mutex
	reqs  []string
	times []time.Time
}

func newFakeServer(t *testing.T, clk *fakeClock, steps ...step) *fakeServer {
	t.Helper()
	fs := &fakeServer{}
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fs.mu.Lock()
		n := len(fs.reqs)
		fs.reqs = append(fs.reqs, r.URL.RequestURI())
		fs.times = append(fs.times, clk.Now())
		fs.mu.Unlock()
		if n >= len(steps) {
			t.Errorf("unexpected request #%d: %s", n+1, r.URL.RequestURI())
			http.Error(w, "unscripted", http.StatusInternalServerError)
			return
		}
		s := steps[n]
		body := s.body
		if s.fixture != "" {
			body = fixture(t, s.fixture)
		}
		if s.status == 0 {
			s.status = http.StatusOK
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(s.status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(fs.Close)
	return fs
}

func (fs *fakeServer) requests() []string {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]string(nil), fs.reqs...)
}

func (fs *fakeServer) arrivals() []time.Time {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return append([]time.Time(nil), fs.times...)
}

// logRecord is one structured log line.
type logRecord struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
}

// logBuffer collects JSON slog output.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) records(t *testing.T) []logRecord {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []logRecord
	for _, line := range strings.Split(strings.TrimSpace(b.buf.String()), "\n") {
		if line == "" {
			continue
		}
		var r logRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		out = append(out, r)
	}
	return out
}

// testClient builds a Client against srv with the fake clock and a captured
// logger. Every test goes through here, so none can reach the real API.
func testClient(t *testing.T, srv *fakeServer, clk *fakeClock, cfg Config, opts ...Option) (*Client, *logBuffer) {
	t.Helper()
	cfg.BaseURL = srv.URL
	if cfg.Symbols == nil {
		cfg.Symbols = []string{"USD_JPY"}
	}
	logs := &logBuffer{}
	all := append([]Option{WithClock(clk), WithLogger(slog.New(slog.NewJSONHandler(logs, nil)))}, opts...)
	c, err := New(cfg, all...)
	if err != nil {
		t.Fatal(err)
	}
	return c, logs
}

// advance moves virtual time without recording a Sleep (time passing on its
// own between calls).
func (f *fakeClock) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// roundTripFunc is an http.RoundTripper made of a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
