package router

import (
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// useMiddleware installs the global middleware chain on engine, in the
// order the comments below require, and returns the SecretsStore the
// Settings/Setup handlers should use (the empty settings.StaticSecretsStore
// when o.secretsStore is unset).
func useMiddleware(engine *gin.Engine, o options) settings.SecretsStore {
	// First, so Recovery's logged 500 is what RequestLog records and even
	// Session's rejections are logged (issues #109/#122).
	engine.Use(middleware.RequestLog(), middleware.Recovery(shared.RenderErrorPage))
	if o.wsBase != "" {
		engine.Use(middleware.WebSocketBase(o.wsBase))
	}
	// Before Session, so a DNS-rebinding request is refused before it can
	// receive the session cookie or a CSRF token (issue #136).
	if o.allowedHosts != nil {
		engine.Use(middleware.HostGuard(o.allowedHosts))
	}
	// Registered next, before SetupGuard, so it covers every route
	// (`/static` excepted inside): the session cookie + CSRF token are
	// required for all state-changing methods and WebSocket upgrades, and
	// the Setup screen's `POST`/`DELETE /settings/:key` (which SetupGuard
	// lets through unauthenticated) are protected too (issues #90/#98/#99).
	engine.Use(middleware.NewSession(shared.RenderErrorPage).Handler())
	// After Session (needs its Authenticated flag) and before SetupGuard, so
	// a page that only redirects to `/setup` still counts as operator
	// activity.
	if o.heartbeatRecorder != nil {
		engine.Use(middleware.Heartbeat(o.heartbeatRecorder))
	}
	settingsStore := o.secretsStore
	if settingsStore == nil {
		settingsStore = settings.StaticSecretsStore{}
	} else {
		// Registered before any route so it also covers NoRoute and the
		// `/api/v1` group; StaticFS below is exempt inside the guard.
		engine.Use(middleware.SetupGuard(settingsStore, config.RequiredSecretKeys()))
	}
	// Header (every full page) SSRs the Kill Switch panel's state and
	// allowed actions from this; it reads lazily, so /static, fragments,
	// APIs and WebSockets pay nothing.
	engine.Use(middleware.SystemState(o.systemEngine))
	return settingsStore
}
