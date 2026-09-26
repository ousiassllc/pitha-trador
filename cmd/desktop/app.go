package main

import "context"

// App is the Wails-bound application struct. It holds the Wails runtime
// context so that later sub-scopes (Kill Switch native notifications, tray
// icon updates, ...) can call runtime.EventsEmit and other Wails runtime
// APIs (docs/architecture/overview.md §9).
type App struct {
	ctx context.Context
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// startup is called by Wails when the app starts. The context it receives
// is required for all Wails runtime calls, so it is saved for later use.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}
