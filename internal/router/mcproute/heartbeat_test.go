package mcproute_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/ousiassllc/pitha-trador/internal/router"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

type heartbeatCounter struct{ calls int }

func (c *heartbeatCounter) RecordHeartbeat(context.Context, time.Time) error {
	c.calls++
	return nil
}

// startingAppWithHeartbeats returns an app whose MCP panel reads "starting"
// (so it polls) and whose router counts the dead-man's switch heartbeats.
func startingAppWithHeartbeats(t *testing.T) (*app, *heartbeatCounter) {
	t.Helper()
	a := newApp(t, fullSecrets{})
	a.seedValid(t)
	a.session()
	if rec := a.post("/settings/mcp/start", url.Values{}, true); rec.Code != http.StatusOK {
		t.Fatalf("start = %d", rec.Code)
	}
	a.dialOK = false // alive, not listening yet: the panel polls
	counter := &heartbeatCounter{}
	a.engine = router.New(router.WithSecretsStore(fullSecrets{}), router.WithMCPSettings(a.svc), router.WithHeartbeatRecorder(counter))
	a.session() // the cookie-less /setup bootstrap records nothing
	if counter.calls != 0 {
		t.Fatalf("session bootstrap recorded %d heartbeats, want 0", counter.calls)
	}
	return a, counter
}

// pollingRequest rebuilds, from the panel's own markup, the request htmx fires
// every 2s: the hx-get URL plus the hx-headers JSON.
func pollingRequest(t *testing.T, body string) (path string, headers map[string]string) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.ElementNode {
			attrs := map[string]string{}
			for _, a := range n.Attr {
				attrs[a.Key] = a.Val
			}
			if strings.HasPrefix(attrs["hx-trigger"], "every ") {
				path, headers = attrs["hx-get"], map[string]string{}
				if raw := attrs["hx-headers"]; raw != "" {
					if err := json.Unmarshal([]byte(raw), &headers); err != nil {
						t.Fatalf("hx-headers %q: %v", raw, err)
					}
				}
				return true
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	if !walk(doc) {
		t.Fatalf("no `every Ns` element in the starting panel: %s", body)
	}
	return path, headers
}

func (a *app) getWith(path string, headers map[string]string) *httptest.ResponseRecorder {
	a.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.AddCookie(a.cookie)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	a.engine.ServeHTTP(rec, req)
	return rec
}

// Issue #1210: the starting panel's 2s self-poll fires with nobody at the
// screen, so it must not refresh last_ui_heartbeat_at (FR-RISK-6).
func TestStartingPanelPollDoesNotRecordHeartbeat(t *testing.T) {
	a, counter := startingAppWithHeartbeats(t)
	// Fetch the markup as a background request so fetching it records nothing.
	rec := a.getWith("/settings/mcp-panel", map[string]string{middleware.BackgroundHeader: "1"})
	path, headers := pollingRequest(t, rec.Body.String())

	for range 3 {
		if got := a.getWith(path, headers); got.Code != http.StatusOK {
			t.Fatalf("poll %s = %d", path, got.Code)
		}
	}
	if counter.calls != 0 {
		t.Fatalf("the panel's own poll recorded %d heartbeats, want 0 (headers sent: %v)", counter.calls, headers)
	}
}

// The same panel fetched as an operator action (no marker) still counts.
func TestPanelRequestWithoutBackgroundMarkerStillRecordsHeartbeat(t *testing.T) {
	a, counter := startingAppWithHeartbeats(t)
	if rec := a.getWith("/settings/mcp-panel", nil); rec.Code != http.StatusOK {
		t.Fatalf("GET /settings/mcp-panel = %d", rec.Code)
	}
	if counter.calls != 1 {
		t.Fatalf("operator request recorded %d heartbeats, want 1", counter.calls)
	}
}
