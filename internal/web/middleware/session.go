package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const (
	// SessionCookieName is the HttpOnly/SameSite=Strict cookie carrying the
	// per-process local session token (docs/api/endpoints.md §1).
	SessionCookieName = "pitha_session"
	// CSRFHeader is the request header every state-changing request must
	// carry the CSRF token in (htmx via `<body hx-headers>`, Lit components
	// via static/src/components/lib/api.ts).
	CSRFHeader = "X-CSRF-Token"
)

type (
	csrfContextKey          struct{}
	authenticatedContextKey struct{}
)

// CSRFToken returns the CSRF token Session put on ctx (the request context
// of a request that went through Session.Handler), or "" when there is
// none. layout.Shell renders it into `<meta name="csrf-token">` and
// `<body hx-headers>`.
func CSRFToken(ctx context.Context) string {
	token, _ := ctx.Value(csrfContextKey{}).(string)
	return token
}

// Authenticated reports whether ctx belongs to a request that carried the
// valid session cookie of a Session.Handler, i.e. one issued by this
// process to the operator's browser/WebView (FR-RISK-6 "認証済みUIリクエスト").
// A request that merely receives the cookie on its first response does not
// count, and neither does one that never went through Session.Handler.
func Authenticated(ctx context.Context) bool {
	ok, _ := ctx.Value(authenticatedContextKey{}).(bool)
	return ok
}

// Session holds the random tokens generated at startup (issues #90, #98,
// #99): a local session token handed to the browser/WebView as an
// HttpOnly, SameSite=Strict cookie, and a separate CSRF token that is only
// ever readable from rendered HTML and must be echoed back in CSRFHeader.
// A cross-site page can neither send the cookie (SameSite=Strict) nor read
// the CSRF token (same-origin policy), so a state-changing request needs
// both.
type Session struct {
	sessionToken string
	csrfToken    string
}

// NewSession generates a fresh Session with new random tokens.
func NewSession() *Session {
	return &Session{sessionToken: rand.Text(), csrfToken: rand.Text()}
}

// Handler returns Gin middleware enforcing the Session on every route
// except `/static/...`:
//
//   - The context always receives the CSRF token (see CSRFToken) and
//     whether the request carried a valid session cookie (see
//     Authenticated).
//   - Safe requests (GET/HEAD/OPTIONS) without a valid session cookie are
//     answered normally and receive the cookie: this is how the browser or
//     WebView obtains it on its first page load, including after an app
//     restart invalidated an old cookie. WebSocket upgrades never receive
//     it, since a socket opened by a page that has no cookie yet is not one
//     this app served.
//   - Every other method (POST/PUT/PATCH/DELETE, ...) and every WebSocket
//     upgrade needs the session cookie; state-changing methods also need
//     CSRFHeader to match the CSRF token. Failures answer 403.
//
// Install it before SetupGuard so the Setup screen and its `POST`/`DELETE
// /settings/:key` routes, which the guard lets through, are protected too.
func (s *Session) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if isStaticPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		hasCookie := tokensEqual(cookieValue(c.Request), s.sessionToken)
		ctx := context.WithValue(c.Request.Context(), csrfContextKey{}, s.csrfToken)
		ctx = context.WithValue(ctx, authenticatedContextKey{}, hasCookie)
		c.Request = c.Request.WithContext(ctx)
		switch {
		case isSafeMethod(c.Request.Method) && !isWebSocketUpgrade(c.Request):
			if !hasCookie {
				s.setCookie(c)
			}
		case !hasCookie:
			forbid(c, "missing or invalid session cookie")
			return
		case !isSafeMethod(c.Request.Method) && !tokensEqual(c.GetHeader(CSRFHeader), s.csrfToken):
			forbid(c, "missing or invalid CSRF token")
			return
		}
		c.Next()
	}
}

func (s *Session) setCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	// No Max-Age: a session cookie, dropped when the WebView/browser closes.
	c.SetCookie(SessionCookieName, s.sessionToken, 0, "/", "", false, true)
}

func forbid(c *gin.Context, reason string) {
	c.String(http.StatusForbidden, "forbidden: "+reason)
	c.Abort()
}

func cookieValue(r *http.Request) string {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func tokensEqual(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func isStaticPath(path string) bool {
	return path == "/static" || strings.HasPrefix(path, "/static/")
}
