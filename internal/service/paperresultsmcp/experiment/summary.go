package experiment

import (
	"context"
	"errors"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Codes of Summary.Error: why one row of the list could not be summarised.
const (
	// SummaryLimitExceeded: a period holds more closed trades than one
	// aggregate may read (query.MaxClosedScan), so the verdict cannot be
	// computed. experiment_compare of that experiment fails the same way.
	SummaryLimitExceeded = "limit_exceeded"
	// SummaryFailed: any other failure of this one row (the cause is in Cause).
	SummaryFailed = "failed"
)

// Summary is one row of experiments_list. When the row could not be
// summarised Error is set and Verdict is empty; the trade counts are still
// there when they could be counted.
type Summary struct {
	Experiment      Experiment `json:"experiment"`
	MinTrades       int        `json:"min_trades"`
	MinTradesSource string     `json:"min_trades_source"`
	// BeforeTrades / AfterTrades are the closed trades of the two periods.
	BeforeTrades int    `json:"before_trades"`
	AfterTrades  int    `json:"after_trades"`
	Confounded   bool   `json:"confounded"`
	Verdict      string `json:"verdict"`
	Judged       bool   `json:"judged"`
	// Error is a Summary* code when this row failed, so that one failing
	// experiment does not take the rest of the list with it.
	Error string `json:"error,omitempty"`
	// Cause is the error behind Error, for the caller's log. Never serialised.
	Cause error `json:"-"`
}

// Summarize is the list row of exp: what Compare would verdict, without
// reading the trades it does not need. The trade counts are SQL COUNTs
// (query.CountClosed), the confounders and the switch check are read as in
// Compare, and the rules that withhold the verdict without a test (aborted,
// confounded, switch unconfirmed, too few trades; see withheld) are applied to
// those. Only an experiment that gets past them has its trades read, for the
// test. The verdict is always the one experiment_compare gives.
//
// A failure of this one experiment is the row's Error, not the call's: the
// returned error is only for a min_trades that is not allowed and for a
// cancelled or expired ctx.
func Summarize(ctx context.Context, q query.Q, exp Experiment, now time.Time, minTrades int, capitalJPY float64) (Summary, error) {
	c, err := newComparison(exp, minTrades)
	if err != nil {
		return Summary{}, err
	}
	s := Summary{Experiment: exp, MinTrades: c.MinTrades, MinTradesSource: c.MinTradesSource}
	if err := summarize(ctx, q, &c, &s, now, capitalJPY); err != nil {
		if ctx.Err() != nil {
			return Summary{}, err
		}
		s.Error, s.Cause = SummaryFailed, err
		if errors.Is(err, query.ErrLimitExceeded) {
			s.Error = SummaryLimitExceeded
		}
	}
	return s, nil
}

func summarize(ctx context.Context, q query.Q, c *Comparison, s *Summary, now time.Time, capitalJPY float64) error {
	before, after := c.Experiment.windows(now)
	var err error
	if s.BeforeTrades, err = query.CountClosed(ctx, q, before); err != nil {
		return err
	}
	if s.AfterTrades, err = query.CountClosed(ctx, q, after); err != nil {
		return err
	}
	c.Before.Performance.ClosedTradeCount, c.After.Performance.ClosedTradeCount = s.BeforeTrades, s.AfterTrades
	if err := c.examine(ctx, q, before, after); err != nil {
		return err
	}
	s.Confounded = c.Confounded
	if v := withheld(*c); v != "" {
		s.Verdict = v
		return nil
	}
	if s.BeforeTrades > query.MaxClosedScan || s.AfterTrades > query.MaxClosedScan {
		return query.ErrLimitExceeded
	}
	var beforePnls, afterPnls []float64
	if c.Before, beforePnls, err = period(ctx, q, before, capitalJPY, false); err != nil {
		return err
	}
	if c.After, afterPnls, err = period(ctx, q, after, capitalJPY, false); err != nil {
		return err
	}
	c.finish(beforePnls, afterPnls)
	s.Verdict, s.Judged = c.Verdict, c.Judged
	return nil
}

// SummarizeAll lists up to limit experiments (newest first, status "" for all)
// with their verdicts. A row that cannot be summarised carries its Error and
// the others are still listed. An experiment that ended long enough ago is
// answered from cache (SummaryCache; nil caches nothing).
func SummarizeAll(ctx context.Context, q query.Q, status string, limit int, now time.Time, minTrades int, capitalJPY float64, cache *SummaryCache) ([]Summary, error) {
	c, err := newComparison(Experiment{}, minTrades) // refuses a min_trades that is not allowed, cached rows or not
	if err != nil {
		return nil, err
	}
	exps, err := List(ctx, q, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(exps))
	for _, exp := range exps {
		if s, ok := cache.get(exp, c.MinTrades); ok {
			out = append(out, s)
			continue
		}
		s, err := Summarize(ctx, q, exp, now, minTrades, capitalJPY)
		if err != nil {
			return nil, err
		}
		cache.put(s, now)
		out = append(out, s)
	}
	return out, nil
}
