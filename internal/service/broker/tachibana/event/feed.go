package event

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/market"
)

const (
	// MaxSymbols is the EVENT subscription cap.
	MaxSymbols = 120

	// kpTimeout: KP arrives after 5 seconds without a notification, so
	// silence for three times that means the connection is dead.
	kpTimeout = 15 * time.Second

	backoffMin = 2 * time.Second
	backoffMax = 5 * time.Minute
	// healthyAfter: a connection that lived this long resets the backoff.
	healthyAfter = time.Minute

	dialTimeout = 15 * time.Second
	// maxFrameBytes bounds one notification (an FD snapshot of 120 symbols
	// with ten-level boards is ~100 KiB).
	maxFrameBytes = 4 << 20
)

// Config configures a Feed.
type Config struct {
	Client *tachibana.Client
	// ConnectionBudget is the number of connections per JST day that planned
	// subscription swaps may use (recovery reconnects are never refused).
	ConnectionBudget int
	// RefillInterval is the least time between two REST 時価 refills.
	RefillInterval time.Duration
	// RefillRequests is how many 時価 requests (120 symbols each) one refill
	// may send; default 1.
	RefillRequests int
	// HTTPClient is used for the WebSocket handshake; default
	// tachibana.NewHTTPClient (IPv4 only).
	HTTPClient *http.Client
	// Clock overrides the wall clock (tests).
	Clock tachibana.Clock
}

// Feed is the EVENT I/F receiver and broker.StreamFeed's Latest.
type Feed struct {
	client         *tachibana.Client
	clock          tachibana.Clock
	hc             *http.Client
	budget         int
	refillInterval time.Duration
	refillSymbols  int // symbols one refill may name

	wake chan struct{} // cap 1: the desired subscription changed

	mu         sync.Mutex // guards the fields below
	desired    []string
	subscribed []string // of the live connection; nil while there is none
	states     map[string]*symbolState
	lastENO    int64
	connDay    string
	connCount  int
	overWarned string // day the over-budget warning was last given
	swapWarned string // day the suspended-swap warning was last given

	refillMu   sync.Mutex // serializes refills; guards lastRefill
	lastRefill time.Time

	rec recorder
}

// New returns a Feed; Run starts it.
func New(cfg Config) *Feed {
	hc := cfg.HTTPClient
	if hc == nil {
		hc = tachibana.NewHTTPClient()
	}
	budget := cfg.ConnectionBudget
	if budget <= 0 {
		budget = tachibanasource.DefaultTachibanaEventMaxConnects
	}
	interval := cfg.RefillInterval
	if interval <= 0 {
		interval = time.Duration(tachibanasource.DefaultTachibanaRestQuoteMinInterval) * time.Second
	}
	requests := cfg.RefillRequests
	if requests <= 0 {
		requests = tachibanasource.DefaultTachibanaRestQuoteRequests
	}
	return &Feed{
		client:         cfg.Client,
		clock:          tachibana.OrReal(cfg.Clock),
		hc:             hc,
		budget:         budget,
		refillInterval: interval,
		refillSymbols:  requests * market.MaxSymbolsPerRequest,
		wake:           make(chan struct{}, 1),
		states:         map[string]*symbolState{},
	}
}

var symbolPattern = regexp.MustCompile(`^[0-9A-Za-z]{1,8}$`)

// SetWatch sets the symbols to receive (at most 120; others are dropped with
// a warning). The subscription is fixed per connection, so it changes only by
// reconnecting: new symbols cause one batched reconnect (counted against the
// daily budget; once it is used up the swap is suspended with a warning),
// symbols that left the list never do - they are dropped at the next
// connection for any other reason.
func (f *Feed) SetWatch(_ context.Context, symbols []string) error {
	var list []string
	for _, s := range symbols {
		s = strings.TrimSpace(s)
		switch {
		case !symbolPattern.MatchString(s):
			slog.Warn("tachibana: EVENT watch list: ignoring a malformed symbol code")
		case !slices.Contains(list, s):
			list = append(list, s)
		}
	}
	if len(list) > MaxSymbols {
		slog.Warn("tachibana: EVENT watch list exceeds the subscription cap; dropping the rest", "symbols", len(list), "cap", MaxSymbols)
		list = list[:MaxSymbols]
	}
	f.mu.Lock()
	f.desired = list
	f.mu.Unlock()
	select {
	case f.wake <- struct{}{}:
	default:
	}
	return nil
}

// UseWatchlist is a no-op: there is no universe-based registration to switch
// away from (unlike kabu's PUSH).
func (f *Feed) UseWatchlist() {}

// Subscribed is the symbol list of the live connection (nil without one).
func (f *Feed) Subscribed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.subscribed)
}

// ConnectionsToday is how many connection attempts were made on the current
// JST day.
func (f *Feed) ConnectionsToday() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.connDay != tachibana.DayKey(f.clock.Now()) {
		return 0
	}
	return f.connCount
}

func (f *Feed) watchList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.desired)
}

// watchSetLocked is what Latest refills: the live subscription, else the
// wanted one.
func (f *Feed) watchSetLocked() []string {
	if len(f.subscribed) > 0 {
		return f.subscribed
	}
	return f.desired
}

func (f *Feed) setSubscribed(symbols []string) {
	f.mu.Lock()
	f.subscribed = symbols
	f.mu.Unlock()
}

func (f *Feed) eventNo() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastENO
}

func (f *Feed) noteEventNo(n int64) {
	f.mu.Lock()
	f.lastENO = max(f.lastENO, n)
	f.mu.Unlock()
}

// wait blocks for d, returning early when ctx ends or the session changes.
func (f *Feed) wait(ctx context.Context, d time.Duration, changed <-chan struct{}) {
	t := f.clock.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
	case <-t.C():
	case <-changed:
	}
}

// Run keeps the EVENT connection until ctx is done: it connects with the
// current virtual URL and watch list, reconnects with backoff after any
// failure (never re-logging in by itself: only an ST with p_errno=2 reports
// the session lost), and reconnects at once after a new login or a planned
// swap. It returns after closing the connection.
func (f *Feed) Run(ctx context.Context) {
	backoff := backoffMin
	reason := "initial"
	for ctx.Err() == nil {
		changed := f.client.SessionChanged()
		sess, ok := f.client.EventSession()
		symbols := f.watchList()
		if !ok || len(symbols) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-changed:
			case <-f.wake:
			}
			continue
		}
		lived, err := f.serve(ctx, sess, changed, symbols, reason)
		f.setSubscribed(nil)
		if ctx.Err() != nil {
			return
		}
		if err == nil { // a new login or a planned swap: reconnect right away
			reason, backoff = "swap", backoffMin
			continue
		}
		reason = "recovery"
		if lived >= healthyAfter {
			backoff = backoffMin
		}
		slog.Warn("tachibana: EVENT connection ended; reconnecting with the same virtual URL", "retry_in", backoff.String(), "lived", lived.Round(time.Second).String(), "error", err)
		f.wait(ctx, backoff, changed)
		backoff = min(backoff*2, backoffMax)
	}
}
