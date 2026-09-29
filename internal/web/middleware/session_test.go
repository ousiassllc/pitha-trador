package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// sessionEngine serves `GET /page` (echoing the CSRF token), a state-changing
// route on every method, a WebSocket-like GET `/ws` and `/static/x`.
func sessionEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(middleware.NewSession().Handler())
	engine.GET("/page", func(c *gin.Context) { c.String(http.StatusOK, middleware.CSRFToken(c.Request.Context())) })
	engine.GET("/ws", func(c *gin.Context) { c.String(http.StatusOK, "upgraded") })
	engine.GET("/static/x", func(c *gin.Context) { c.String(http.StatusOK, "asset") })
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		engine.Handle(method, "/act", func(c *gin.Context) { c.String(http.StatusOK, "done") })
	}
	return engine
}

func do(engine *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}

// login performs the first page load a browser does and returns the session
// cookie it was given plus the CSRF token the page would render.
func login(t *testing.T, engine *gin.Engine) (*http.Cookie, string) {
	t.Helper()
	rec := do(engine, httptest.NewRequest(http.MethodGet, "/page", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != middleware.SessionCookieName {
		t.Fatalf("first GET cookies = %v, want exactly the %s cookie", cookies, middleware.SessionCookieName)
	}
	return cookies[0], rec.Body.String()
}

func request(method, path string, cookie *http.Cookie, csrf string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	if csrf != "" {
		req.Header.Set(middleware.CSRFHeader, csrf)
	}
	return req
}

func TestSession_FirstSafeRequestReceivesHardenedCookie(t *testing.T) {
	cookie, csrf := login(t, sessionEngine(t))

	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Value == "" {
		t.Fatalf("cookie = %+v, want non-empty HttpOnly SameSite=Strict Path=/", cookie)
	}
	if csrf == "" || csrf == cookie.Value {
		t.Fatalf("csrf token %q must be non-empty and distinct from the session token", csrf)
	}
}

func TestSession_ValidCookieIsNotReissued(t *testing.T) {
	engine := sessionEngine(t)
	cookie, _ := login(t, engine)

	rec := do(engine, request(http.MethodGet, "/page", cookie, ""))
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("Set-Cookie on request with valid cookie = %v, want none", got)
	}
}

func TestSession_StateChangingMethodsRequireCookieAndCSRFToken(t *testing.T) {
	engine := sessionEngine(t)
	cookie, csrf := login(t, engine)
	stale := &http.Cookie{Name: middleware.SessionCookieName, Value: "stale"}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, tc := range []struct {
			name   string
			cookie *http.Cookie
			csrf   string
			want   int
		}{
			{"no cookie no token", nil, "", http.StatusForbidden},
			{"token but no cookie", nil, csrf, http.StatusForbidden},
			{"cookie but no token", cookie, "", http.StatusForbidden},
			{"cookie but wrong token", cookie, "wrong", http.StatusForbidden},
			{"session token used as csrf token", cookie, cookie.Value, http.StatusForbidden},
			{"stale cookie", stale, csrf, http.StatusForbidden},
			{"cookie and token", cookie, csrf, http.StatusOK},
		} {
			rec := do(engine, request(method, "/act", tc.cookie, tc.csrf))
			if rec.Code != tc.want {
				t.Errorf("%s %s: status = %d, want %d", method, tc.name, rec.Code, tc.want)
			}
		}
	}
}

func TestSession_RejectedRequestNeverReachesHandlerNorGetsCookie(t *testing.T) {
	engine := sessionEngine(t)
	rec := do(engine, request(http.MethodPost, "/act", nil, ""))
	if rec.Body.String() == "done" {
		t.Fatal("handler ran for a request without cookie/token")
	}
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("rejected POST issued cookie %v; only safe page loads may", got)
	}
}

func TestSession_WebSocketUpgradeRequiresCookie(t *testing.T) {
	engine := sessionEngine(t)
	cookie, _ := login(t, engine)

	upgrade := func(c *http.Cookie) *http.Request {
		req := request(http.MethodGet, "/ws", c, "")
		req.Header.Set("Upgrade", "websocket")
		return req
	}
	rec := do(engine, upgrade(nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("upgrade without cookie: status = %d, want %d", rec.Code, http.StatusForbidden)
	}
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("upgrade without cookie issued cookie %v", got)
	}
	if rec := do(engine, upgrade(cookie)); rec.Code != http.StatusOK {
		t.Fatalf("upgrade with cookie: status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestSession_StaticAssetsAreExempt(t *testing.T) {
	rec := do(sessionEngine(t), request(http.MethodGet, "/static/x", nil, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("static asset issued cookie %v", got)
	}
}

func TestSession_TokensDifferPerInstance(t *testing.T) {
	a, _ := login(t, sessionEngine(t))
	b, _ := login(t, sessionEngine(t))
	if a.Value == b.Value {
		t.Fatalf("two Sessions issued the same token %q", a.Value)
	}
	// A cookie from a previous process must not authorize state changes.
	engine := sessionEngine(t)
	_, csrf := login(t, engine)
	if rec := do(engine, request(http.MethodPost, "/act", a, csrf)); rec.Code != http.StatusForbidden {
		t.Fatalf("cookie from another Session: status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
