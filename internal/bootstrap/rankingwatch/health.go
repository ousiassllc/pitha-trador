package rankingwatch

import (
	"errors"
	"log/slog"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketdata"
)

// errorLogEvery is the minimum gap between two repeated failure lines of the
// same kind; recovery is logged at once.
const errorLogEvery = 10 * time.Minute

// health logs each cycle's outcome with counts, durations and error codes only
// - never a price or a symbol of the ranking (kabusapi#1343) - and keeps
// repeating failures from flooding the log.
type health struct {
	failing    bool
	failures   int
	lastLogged time.Time
	lastEmpty  time.Time
	wasEmpty   bool
	other      map[string]time.Time
}

// report logs the cycle's ranking outcome.
func (h *health) report(now time.Time, res rankingResult, held, watch, added, removed int, took time.Duration) {
	attrs := []any{
		"ranked_rows", res.rows, "ranked_in_universe", len(res.symbols), "held", held,
		"watch", watch, "added", added, "removed", removed, "duration_ms", took.Milliseconds(),
	}
	switch {
	case res.offSession:
		h.recovered()
	case res.err != nil:
		h.failures++
		h.failing = true
		if h.lastLogged.IsZero() || now.Sub(h.lastLogged) >= errorLogEvery {
			h.lastLogged = now
			slog.Warn("rankingwatch: ranking failed, zero ranked candidates until the next cycle",
				append(attrs, "consecutive_failures", h.failures, "error_code", errorCode(res.err), "rate_limited", marketdata.IsRateLimit(res.err) || errors.Is(res.err, marketdata.ErrRateLimited), "error", res.err)...)
		}
	case len(res.symbols) == 0:
		h.recovered()
		// kabu returns an empty ranking on weekdays from about 7:53 until just after 9:00.
		if !h.wasEmpty || now.Sub(h.lastEmpty) >= errorLogEvery {
			h.lastEmpty = now
			slog.Info("rankingwatch: ranking is empty, zero ranked candidates (held symbols only)", attrs...)
		}
		h.wasEmpty = true
	default:
		h.recovered()
		if h.wasEmpty {
			slog.Info("rankingwatch: ranking is back", attrs...)
		}
		h.wasEmpty = false
		slog.Info("rankingwatch: watch list updated", attrs...)
	}
}

// recovered ends a failure streak with one log line.
func (h *health) recovered() {
	if h.failing {
		slog.Info("rankingwatch: ranking recovered", "after_failures", h.failures)
	}
	h.failing, h.failures, h.lastLogged = false, 0, time.Time{}
}

func (h *health) logUniverseError(now time.Time) { h.logOther(now, "universe", "list universe failed") }
func (h *health) logHeldError(now time.Time) {
	h.logOther(now, "held", "list held symbols failed, keeping the previous ones")
}
func (h *health) logEnqueueError(now time.Time, err error) {
	h.logOther(now, "enqueue", "enqueue market-data jobs failed", "error", err)
}
func (h *health) logRegisterError(now time.Time, err error) {
	h.logOther(now, "register", "PUSH registration failed, retrying next cycle", "error", err)
}

// logOther logs a non-ranking failure at most once per errorLogEvery per kind.
func (h *health) logOther(now time.Time, kind, msg string, attrs ...any) {
	if last, ok := h.other[kind]; ok && now.Sub(last) < errorLogEvery {
		return
	}
	if h.other == nil {
		h.other = make(map[string]time.Time)
	}
	h.other[kind] = now
	slog.Warn("rankingwatch: "+msg, attrs...)
}

// errorCode is the kabu error code of err, or 0.
func errorCode(err error) int {
	var api *marketdata.APIError
	if errors.As(err, &api) {
		return api.Code
	}
	return 0
}
