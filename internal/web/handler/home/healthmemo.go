package home

import (
	"context"
	"strings"
	"sync"
	"time"
)

// healthTTL is how long one Health read is reused. The Home page's cards (and
// the header badge) are separate fragments that htmx re-fetches on the same
// 30 s cadence, and each used to run health_today's ~10 queries on its own;
// within this window they share one read.
const healthTTL = 3 * time.Second

// healthMemo remembers the last successful Health read.
type healthMemo struct {
	mu     sync.Mutex
	valid  bool
	at     time.Time
	basis  string
	health Health
}

// health returns today's Health, reusing the memoized one when it is younger
// than healthTTL and was read under the same basis.
//
// health_today's attention codes restate the live state the caller has just
// read cheaply (Kill Switch, daily halt, kabu alerts). The basis is a summary
// of those reads: when one changed (a Kill Switch pause/resume, a halt, an
// alert appearing or clearing - the very moments the page refreshes its
// fragments), the memo is skipped, so a fragment never combines a fresh state
// with a code of the old one. A failed read is never memoized.
func (h *Handler) health(ctx context.Context, now time.Time, basis string) (Health, error) {
	m := &h.memo
	m.mu.Lock()
	defer m.mu.Unlock()
	if age := now.Sub(m.at); m.valid && age >= 0 && age < healthTTL && m.basis == basis {
		return m.health, nil
	}
	hl, err := h.deps.Source.Health(ctx, now)
	if err != nil {
		m.valid = false
		return hl, err
	}
	m.valid, m.at, m.basis, m.health = true, now, basis, hl
	return hl, nil
}

// healthBasis summarizes the live inputs health_today's attention codes
// restate: the system state, the daily halt, the active health alerts and the
// broker session's connection state.
func healthBasis(in statusInputs) string {
	var b strings.Builder
	b.WriteString(string(in.state))
	b.WriteString("|")
	if in.stateErr != nil {
		b.WriteString("state-err|")
	}
	if in.halt != nil {
		b.WriteString("halt|")
	}
	for _, a := range in.alerts {
		b.WriteString(a.Code)
		b.WriteString(",")
	}
	b.WriteString("|")
	b.WriteString(in.market.Guidance)
	b.WriteString("|")
	b.WriteString(in.market.Notice)
	return b.String()
}
