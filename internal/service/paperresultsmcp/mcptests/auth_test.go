package mcptests

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/settings"
)

// freeAddr returns a loopback address whose port was free a moment ago.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

func requireRefused(t *testing.T, addr string) {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("connected to %s: something is listening", addr)
	}
}

// Default settings are disabled: Serve returns without binding, so a
// connection attempt is refused (not_listening).
func TestDefaultSettingsAreDisabledAndDoNotListen(t *testing.T) {
	s := enabledSettings()
	s.Enabled = false
	s.APIKey = ""
	d := settings.Default()
	if d.Enabled || d.Listen != "127.0.0.1:48110" || d.APIKey != "" || d.VerifyAccessHeaders {
		t.Fatalf("default settings are not off: %+v", d)
	}

	for name, cfg := range map[string]struct {
		enabled bool
		key     string
	}{"default": {false, ""}, "key set but disabled": {false, testAPIKey}} {
		t.Run(name, func(t *testing.T) {
			addr := freeAddr(t)
			cfgS := s
			cfgS.Listen, cfgS.APIKey = addr, cfg.key
			logs := &lockedBuffer{}
			srv := paperresultsmcp.New(paperresultsmcp.Options{Settings: cfgS, DBPath: filepath.Join(t.TempDir(), "none.db"), Logger: newJSONLogger(logs)})
			ln, err := srv.Listen()
			if ln != nil {
				_ = ln.Close()
				t.Fatal("Listen returned a listener while disabled")
			}
			if !errors.Is(err, paperresultsmcp.ErrNotListening) {
				t.Fatalf("Listen err = %v, want ErrNotListening", err)
			}
			if err := srv.Serve(t.Context()); !errors.Is(err, paperresultsmcp.ErrNotListening) {
				t.Fatalf("Serve err = %v, want ErrNotListening", err)
			}
			requireRefused(t, addr)
			if strings.Contains(logs.String(), testAPIKey) {
				t.Fatal("log leaked the api key")
			}
		})
	}
}

// enabled=true with an empty api_key refuses to listen and logs exactly the
// spec line; no secret (here: the Access values) is logged.
func TestEnabledWithEmptyAPIKeyRefusesToListen(t *testing.T) {
	for name, mutate := range map[string]func(*settings.Settings){
		"empty key": func(s *settings.Settings) {},
		"empty key with access vals": func(s *settings.Settings) {
			s.VerifyAccessHeaders, s.AccessClientID, s.AccessClientSecret = true, "cid", "csecret-value"
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := enabledSettings()
			s.APIKey = ""
			s.Listen = freeAddr(t)
			mutate(&s)
			logs := &lockedBuffer{}
			srv := paperresultsmcp.New(paperresultsmcp.Options{Settings: s, DBPath: "unused", Logger: newJSONLogger(logs)})
			if ln, err := srv.Listen(); ln != nil || !errors.Is(err, paperresultsmcp.ErrNotListening) {
				t.Fatalf("Listen = (%v, %v), want (nil, paperresultsmcp.ErrNotListening)", ln, err)
			}
			requireRefused(t, s.Listen)

			out := logs.String()
			if !strings.Contains(out, `"msg":"paper-results-mcp: refusing to listen: no auth configured"`) {
				t.Fatalf("exact refusal line missing from log: %s", out)
			}
			for _, secret := range []string{"csecret-value", "cid"} {
				if strings.Contains(out, secret) {
					t.Fatalf("log leaked %q: %s", secret, out)
				}
			}
		})
	}
}

// Optional Access verification enabled without an id or secret also refuses
// to listen (an empty comparison must never pass).
func TestAccessVerificationWithoutValuesRefusesToListen(t *testing.T) {
	s := enabledSettings()
	s.Listen = freeAddr(t)
	s.VerifyAccessHeaders, s.AccessClientID = true, "only-id"
	srv := paperresultsmcp.New(paperresultsmcp.Options{Settings: s, DBPath: "unused", Logger: newJSONLogger(&lockedBuffer{})})
	if ln, err := srv.Listen(); ln != nil || !errors.Is(err, paperresultsmcp.ErrNotListening) {
		t.Fatalf("Listen = (%v, %v), want (nil, paperresultsmcp.ErrNotListening)", ln, err)
	}
	requireRefused(t, s.Listen)
}

// With a valid configuration Listen binds and the endpoint answers.
func TestEnabledWithKeyListensAndAnswers(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedAll(t, w)
	s := enabledSettings()
	s.Listen = freeAddr(t)
	srv := paperresultsmcp.New(paperresultsmcp.Options{Settings: s, DBPath: path, Now: testkit.FixedNow})
	ln, err := srv.Listen()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() { done <- srv.ServeListener(ctx, ln) }()

	req, _ := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String(), strings.NewReader(rpc("tools/list", nil)))
	req.Header.Set("Authorization", "Bearer "+testAPIKey)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("ServeListener: %v", err)
	}
}

// Missing or wrong Bearer: 401 unauthorized with one fixed message, zero
// SQL statements and zero database connections - even for initialize and
// tools/list, and even when the database file does not exist.
func TestUnauthorizedRequestsReachNoSQLAndOpenNoConnection(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedAll(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})

	secretLooking := "WRONG-KEY-must-not-be-echoed"
	headers := map[string]string{
		"missing header":      "",
		"wrong key":           "Bearer " + secretLooking,
		"wrong length":        "Bearer x",
		"right key as prefix": "Bearer " + testAPIKey + "extra",
		"basic scheme":        "Basic " + testAPIKey,
		"bare key":            testAPIKey,
		"empty bearer":        "Bearer ",
	}
	bodies := map[string]string{
		"initialize": rpc("initialize", map[string]any{"protocolVersion": "2025-06-18"}),
		"tools/list": rpc("tools/list", nil),
		"tools/call": callBody("paper_performance_summary", period),
		// The order-proposal tools (issue #827) need the key like every request.
		"order_proposal_submit":   callBody("order_proposal_submit", map[string]any{"idempotency_key": "proposal-0001"}),
		"order_proposal_get":      callBody("order_proposal_get", map[string]any{"proposal_id": 1}),
		"order_proposal_cancel":   callBody("order_proposal_cancel", map[string]any{"proposal_id": 1}),
		"order_proposals_disable": callBody("order_proposals_disable", map[string]any{}),
		"garbage":                 "not json at all",
	}
	var messages []string
	for hn, h := range headers {
		for bn, body := range bodies {
			t.Run(hn+"/"+bn, func(t *testing.T) {
				r := srv.post(t, h, body)
				r.wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeUnauthorized)
				messages = append(messages, r.errorBody(t).Message)
				if strings.Contains(string(r.body), secretLooking) {
					t.Fatal("response echoed the presented key")
				}
			})
		}
	}
	for _, m := range messages {
		if m != messages[0] {
			t.Fatalf("unauthorized messages differ: %q vs %q", m, messages[0])
		}
	}
	// A GET without credentials is also a plain 401, not a 405 that would
	// reveal the endpoint's method handling.
	req, _ := http.NewRequest(http.MethodGet, srv.http.URL, nil)
	srv.do(t, req).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeUnauthorized)

	if n := counter.Statements.Load(); n != 0 {
		t.Fatalf("%d SQL statements ran for unauthenticated requests: %v", n, counter.StatementLog())
	}
	if n := counter.Opens.Load(); n != 0 {
		t.Fatalf("%d database connections opened for unauthenticated requests", n)
	}
	logs := srv.logs.String()
	if strings.Contains(logs, secretLooking) || strings.Contains(logs, testAPIKey) {
		t.Fatal("log contains a presented or configured key")
	}

	// Positive control: the right key does reach SQL through the same driver.
	srv.call(t, "paper_performance_summary", period).data(t, "paper_performance_summary", new(map[string]any))
	if counter.Statements.Load() == 0 || counter.Opens.Load() != 1 {
		t.Fatalf("control call: statements=%d opens=%d", counter.Statements.Load(), counter.Opens.Load())
	}
}

// A database path that does not exist is never touched for an
// unauthenticated request: the answer is 401, not db_unavailable.
func TestUnauthorizedBeatsMissingDatabase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.db")
	srv := newTestServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: missing})
	srv.post(t, "Bearer wrong", callBody("paper_daily_pnl", period)).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeUnauthorized)
	srv.post(t, "", rpc("tools/list", nil)).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeUnauthorized)
}

// Optional Cloudflare Access check (architecture §3.5): before the key,
// same message, zero SQL, and never required when it is off.
func TestAccessHeaderVerification(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	testkit.SeedAll(t, w)
	driverName, counter := testkit.NewCountingDriver(t)
	s := enabledSettings()
	s.VerifyAccessHeaders, s.AccessClientID, s.AccessClientSecret = true, "cf-id", "cf-secret"
	srv := newTestServer(t, paperresultsmcp.Options{Settings: s, DBPath: path, DriverName: driverName})
	good := "Bearer " + testAPIKey
	post := func(id, secret, auth string) response {
		req, _ := http.NewRequest(http.MethodPost, srv.http.URL, strings.NewReader(callBody("paper_daily_pnl", period)))
		if id != "" {
			req.Header.Set("CF-Access-Client-Id", id)
		}
		if secret != "" {
			req.Header.Set("CF-Access-Client-Secret", secret)
		}
		req.Header.Set("Authorization", auth)
		return srv.do(t, req)
	}
	post("", "cf-secret", good).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeAccessDenied)
	post("cf-id", "", good).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeAccessDenied)
	post("cf-id", "wrong", good).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeAccessDenied)
	post("wrong", "cf-secret", good).wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeAccessDenied)
	post("cf-id", "cf-secret", "Bearer nope").wantError(t, http.StatusUnauthorized, paperresultsmcp.CodeUnauthorized)
	if counter.Statements.Load() != 0 || counter.Opens.Load() != 0 {
		t.Fatal("rejected requests reached the database")
	}
	if r := post("cf-id", "cf-secret", good); r.status != http.StatusOK {
		t.Fatalf("both checks pass: status %d %s", r.status, r.body)
	}
	if strings.Contains(srv.logs.String(), "cf-secret") {
		t.Fatal("log contains the access client secret")
	}
}
