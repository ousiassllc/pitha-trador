// Package mcpbridge is the desktop application's side of the Paper results
// MCP's update tools (issue #1222). The MCP is a separate process without the
// updater, so the application keeps the hand-over of internal/repository/
// updatecontrol: it publishes updater.Status there and executes the requests
// the MCP records.
//
// Nothing of the updater is re-implemented. A check request runs
// Checker.CheckForUpdate, the same call the scheduler's periodic tick makes
// (serialized by its checkMu), but never quits. An apply request launches the
// install only through the application's own QuitForUpdate, and only for an
// installer that this bridge's own check downloaded and verified (signature,
// checksum, URL and size rules all live in the Checker), after SafeGate is
// evaluated again. The scheduler's own behavior is unchanged: a periodic check
// that finds a Ready installer still quits at once.
//
// cmd/server has no updater and never starts a bridge, so the MCP finds no
// published status there and says the build has no updater.
package mcpbridge

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/updatecontrol"
	"github.com/ousiassllc/pitha-trador/internal/safego"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

// Defaults of Config.
const (
	// DefaultTick is how often the bridge looks for new requests and a changed status.
	DefaultTick = time.Second
	// DefaultHeartbeat is how often the status is rewritten without a change;
	// the MCP treats a status older than updatecontrol.StaleAfter as "no updater".
	DefaultHeartbeat = 10 * time.Second
	// DefaultMaxQueueAge is how long a request may wait before it is dropped unexecuted
	// (the MCP gave up on it by then).
	DefaultMaxQueueAge = 30 * time.Second
)

// Checker is the part of *updater.Checker the bridge uses.
type Checker interface {
	CheckForUpdate(ctx context.Context) (updater.Result, error)
	Status() updater.Status
	SafeToUpdate(ctx context.Context) (bool, updater.BlockReason)
}

// Config configures a Bridge. Store, Checker and Quitter are required.
type Config struct {
	Store   *updatecontrol.Store
	Checker Checker
	// Quitter is the application's QuitForUpdate, the only way an install starts.
	Quitter updater.Quitter

	// The fields below default to the production values; tests set them.
	Version     string
	Now         func() time.Time
	Tick        time.Duration
	Heartbeat   time.Duration
	MaxQueueAge time.Duration
}

// Bridge publishes the updater's status and executes the MCP's requests.
type Bridge struct {
	cfg Config

	mu    sync.Mutex
	ready *updater.Result // the installer this bridge's own check verified; nil when none

	pubMu     sync.Mutex // guards the last published status
	last      updater.Status
	lastAt    time.Time
	published bool
}

// New returns a Bridge.
func New(cfg Config) *Bridge {
	if cfg.Version == "" {
		cfg.Version = version.Version
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Tick <= 0 {
		cfg.Tick = DefaultTick
	}
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = DefaultHeartbeat
	}
	if cfg.MaxQueueAge <= 0 {
		cfg.MaxQueueAge = DefaultMaxQueueAge
	}
	return &Bridge{cfg: cfg}
}

// Run publishes the status and serves requests until ctx is canceled, then
// withdraws the status. Requests a previous run left behind are dropped first.
// Requests are executed one at a time on their own goroutine, so a long
// check (a download) never stops the heartbeat.
func (b *Bridge) Run(ctx context.Context) {
	cleanup := context.WithoutCancel(ctx)
	if err := b.cfg.Store.ExpireUnfinished(cleanup, b.cfg.Now()); err != nil {
		slog.Warn("update bridge: drop stale requests failed", "error", err)
	}
	var wg sync.WaitGroup
	wg.Go(func() { b.serve(ctx) })
	b.publishLoop(ctx)
	wg.Wait()
	if err := b.cfg.Store.Retire(cleanup); err != nil {
		slog.Warn("update bridge: retire status failed", "error", err)
	}
}

func (b *Bridge) publishLoop(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.Tick)
	defer ticker.Stop()
	for {
		b.publish(ctx, false)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// publish writes the Checker's status when it changed, the heartbeat is due
// or force is set.
func (b *Bridge) publish(ctx context.Context, force bool) {
	b.pubMu.Lock()
	defer b.pubMu.Unlock()
	st := b.cfg.Checker.Status()
	now := b.cfg.Now()
	if !force && b.published && st == b.last && now.Sub(b.lastAt) < b.cfg.Heartbeat {
		return
	}
	if err := b.cfg.Store.Publish(ctx, now, b.row(st, now)); err != nil {
		if ctx.Err() == nil { // a canceled publish is the shutdown, not a failure
			slog.Warn("update bridge: publish status failed", "error", err)
		}
		return
	}
	b.last, b.lastAt, b.published = st, now, true
}

// row is st as the published row: kinds only, never LastError.
func (b *Bridge) row(st updater.Status, now time.Time) updatecontrol.Status {
	row := updatecontrol.Status{
		HeartbeatAt: now, CurrentVersion: b.cfg.Version,
		DevBuild: st.DevBuild, NoRelease: st.NoRelease, Available: st.Available, Blocked: st.Blocked,
		BlockedKind: string(st.BlockedKind), Ready: st.Ready, ErrorKind: string(st.ErrorKind),
	}
	if st.Available {
		row.LatestVersion = st.Version
	}
	if !st.CheckedAt.IsZero() {
		checked := st.CheckedAt.UTC()
		row.CheckedAt = &checked
	}
	return row
}

func (b *Bridge) serve(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.Tick)
	defer ticker.Stop()
	for {
		safego.Run("update bridge requests", func() { b.drain(ctx) })
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// drain executes every pending request, oldest first.
func (b *Bridge) drain(ctx context.Context) {
	for ctx.Err() == nil {
		pending, err := b.cfg.Store.HasPending(ctx)
		if err != nil || !pending {
			if err != nil && ctx.Err() == nil {
				slog.Warn("update bridge: look for requests failed", "error", err)
			}
			return
		}
		req, ok, err := b.cfg.Store.Claim(ctx)
		if err != nil {
			slog.Warn("update bridge: claim request failed", "error", err)
			return
		}
		if !ok {
			return
		}
		b.handle(ctx, req)
	}
}

func (b *Bridge) handle(ctx context.Context, req updatecontrol.Request) {
	finish := func(outcome, reason string) error {
		// The caller's ctx may be canceled by the shutdown the answer belongs to.
		err := b.cfg.Store.Finish(context.WithoutCancel(ctx), b.cfg.Now(), req.ID, outcome, reason)
		if err != nil {
			slog.Warn("update bridge: finish request failed", "kind", req.Kind, "error", err)
		}
		return err
	}
	if b.cfg.Now().Sub(req.RequestedAt) > b.cfg.MaxQueueAge {
		_ = finish(updatecontrol.OutcomeExpired, "")
		return
	}
	switch req.Kind {
	case updatecontrol.KindCheck:
		_ = finish(b.check(ctx))
	case updatecontrol.KindApply:
		b.apply(ctx, req, finish)
	default:
		_ = finish(updatecontrol.OutcomeExpired, "")
	}
}

// check is Checker.CheckForUpdate, without the quit SchedulerAdapter adds.
func (b *Bridge) check(ctx context.Context) (outcome, reason string) {
	result, err := b.cfg.Checker.CheckForUpdate(ctx)
	// The new state is published before the request is answered, so the
	// caller reads the status this check produced.
	b.publish(context.WithoutCancel(ctx), true)
	b.mu.Lock()
	defer b.mu.Unlock()
	if err != nil {
		b.ready = nil // a failed check says nothing new about an older installer; start over
		return updatecontrol.OutcomeCheckFailed, ""
	}
	if result.Ready {
		b.ready = &result
	} else {
		b.ready = nil
	}
	return updatecontrol.OutcomeChecked, ""
}

// apply installs the installer b.check verified, when SafeGate still passes.
func (b *Bridge) apply(ctx context.Context, req updatecontrol.Request, finish func(outcome, reason string) error) {
	b.mu.Lock()
	ready := b.ready
	b.mu.Unlock()
	if ready == nil || !b.stillReady(ready) {
		_ = finish(updatecontrol.OutcomeDeferred, updatecontrol.ReasonNotReady)
		return
	}
	if safe, why := b.cfg.Checker.SafeToUpdate(ctx); !safe {
		slog.Info("update bridge: apply deferred by safety gate", "kind", string(why.Kind), "detail", why.Detail)
		_ = finish(updatecontrol.OutcomeDeferred, string(why.Kind))
		return
	}
	// The answer (the audit of the request) is recorded before the application
	// starts to quit: the MCP is stopped first on the way out and would
	// otherwise never learn the outcome. Without the record nothing is installed.
	if err := finish(updatecontrol.OutcomeAccepted, ""); err != nil {
		return
	}
	b.mu.Lock()
	b.ready = nil
	b.mu.Unlock()
	slog.Info("update bridge: install requested through the MCP", "version", ready.Version, "client", req.Client)
	b.cfg.Quitter.QuitForUpdate(ready.InstallerPath)
}

// stillReady is true while the Checker's status still reports the installer
// the bridge holds (a later check that found something else clears it).
func (b *Bridge) stillReady(r *updater.Result) bool {
	st := b.cfg.Checker.Status()
	return st.Ready && st.Version == r.Version
}
