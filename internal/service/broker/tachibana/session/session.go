// Package session keeps the 立花 e支店 login alive (issue #727): one login a
// day, a re-login every morning after the 03:30 close, another only when the
// broker reports the session gone, a logout when the process stops. It turns
// failures into the operator-facing broker.SessionStatus and raises notices
// (Slack, Activity feed) for what needs a human.
package session

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/broker"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
)

const (
	backoffMin = 5 * time.Second
	backoffMax = 5 * time.Minute

	// minReloginInterval is the least time between two logins caused by a lost
	// session (p_errno=2): a re-login invalidates the previous virtual URL, so
	// a tight loop would also fight with itself.
	minReloginInterval = 30 * time.Second
	// contentionWindow: a session lost within it after a login points at
	// another process or tool logging in with the same 認証ID.
	contentionWindow = 2 * time.Minute
	// contentionLosses is how many such quick losses in a row make the status
	// "rejected" and raise the notice.
	contentionLosses = 2
	// lossWindow and maxLossRelogins cap the lost-session logins; past the cap
	// the adapter waits for the next daily re-login instead of fighting on.
	lossWindow      = time.Hour
	maxLossRelogins = 3

	logoutTimeout = 5 * time.Second
)

// phase is where the session loop waits.
type phase int

const (
	// phaseActive: a login is valid; waiting for the 03:30 close.
	phaseActive phase = iota
	// phaseClosed: the session ended at the close (or a loss cap was hit);
	// waiting for the daily re-login time.
	phaseClosed
	// phaseRetry: the last login attempt failed; waiting to try again.
	phaseRetry
)

// Config configures a Session.
type Config struct {
	Client *tachibana.Client
	// AuthID is the 認証ID of the environment.
	AuthID string
	// KeyPath is the 秘密鍵 file, read at every login so that a replaced key is
	// picked up without a restart.
	KeyPath string
	// ReauthTime is the daily re-login time of day, "HH:MM" JST
	// (config.KeyTachibanaReauthTime).
	ReauthTime string
	// APIVersion labels the API version in SessionStatus (e.g. "e_api_v4r10").
	APIVersion string
	// Clock overrides the wall clock (tests); the Client's clock is separate.
	Clock tachibana.Clock
	// Notifier receives operator notices (Slack, Activity); may be nil.
	Notifier Notifier
	// OnLogin runs in the background after every successful login (the
	// master's morning fetch hangs on it). It must return when ctx is done;
	// Wait waits for it. May be nil.
	OnLogin func(ctx context.Context)
}

// Session keeps one login alive: it logs in at start, once every morning
// after the 03:30 close (never mid-day while the session holds), again only
// when the broker reports the session gone, and logs out at the end. Status
// is the operator-facing state.
type Session struct {
	cfg    Config
	client *tachibana.Client
	clock  tachibana.Clock
	reauth time.Duration

	lost chan struct{} // cap 1: a session-lost signal from the client
	wg   sync.WaitGroup

	// Loop-private state (only the run goroutine and Start touch it, never
	// concurrently).
	retryWait     time.Duration
	lastLogin     time.Time
	lossTimes     []time.Time
	quickLosses   int
	overdueNoted  bool
	docsNoted     bool
	prevSpec      string
	prevDocUpdate string

	mu        sync.Mutex // guards the fields below
	status    broker.SessionStatus
	phase     phase
	contended bool
	skewed    bool
}

var _ tachibana.SessionObserver = (*Session)(nil)

// New returns a Session for cfg and registers it as the client's session
// observer.
func New(cfg Config) *Session {
	s := &Session{
		cfg:    cfg,
		client: cfg.Client,
		clock:  tachibana.OrReal(cfg.Clock),
		reauth: tachibana.ParseClockOfDay(cfg.ReauthTime),
		lost:   make(chan struct{}, 1),
	}
	s.status.APIVersion = cfg.APIVersion
	cfg.Client.SetObserver(s)
	return s
}

// Start logs in and keeps the session alive until ctx is done. A failed first
// attempt is returned (sanitized: no credential or URL) and the background
// loop keeps retrying, so the process can come up before the broker is
// reachable. Start must be called once.
func (s *Session) Start(ctx context.Context) error {
	err := s.attempt(ctx)
	s.wg.Add(1)
	go func() { defer s.wg.Done(); s.run(ctx) }()
	return err
}

// Wait blocks until the loop has ended and logged out, which happens after the
// ctx given to Start is done.
func (s *Session) Wait() { s.wg.Wait() }

// LoginNow makes one login attempt right away (a no-op for the schedule: the
// loop keeps its own timing). It is what the loop itself runs at the daily
// re-login time.
func (s *Session) LoginNow(ctx context.Context) error { return s.attempt(ctx) }

// Status is the state of the most recent login (never blocks on the network).
func (s *Session) Status() broker.SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status
	if st.Issue == broker.SessionIssueNone && s.skewed {
		st.Issue, st.Code = broker.SessionIssueClockSkew, tachibana.ErrnoClock
		st.Guidance = guidanceClock
	}
	if st.Issue == broker.SessionIssueNone && s.contended {
		st.Issue = broker.SessionIssueSessionConflict
		st.Guidance = guidanceContention
	}
	return st
}

// SessionLost implements tachibana.SessionObserver.
func (s *Session) SessionLost(uint64) {
	select {
	case s.lost <- struct{}{}:
	default:
	}
}

// ClockSkew implements tachibana.SessionObserver.
func (s *Session) ClockSkew() {
	s.mu.Lock()
	s.skewed = true
	s.mu.Unlock()
	slog.Warn("tachibana: the broker rejected p_sd_date (p_errno=8): synchronize the PC clock with NTP")
}

// RequestSucceeded implements tachibana.SessionObserver.
func (s *Session) RequestSucceeded() {
	now := s.clock.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skewed = false
	if s.contended && now.Sub(s.status.LoggedInAt) > contentionWindow {
		s.contended = false
		s.quickLosses = 0
	}
}

func (s *Session) getPhase() phase {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.phase
}

func (s *Session) setPhase(p phase) {
	s.mu.Lock()
	s.phase = p
	s.mu.Unlock()
}
