package experiment

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Summary is one row of experiments_list.
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
}

// Summarize is Compare reduced to the list row (no risk_rejections).
func Summarize(ctx context.Context, q query.Q, exp Experiment, now time.Time, minTrades int, capitalJPY float64) (Summary, error) {
	c, err := Compare(ctx, q, exp, now, minTrades, capitalJPY, false)
	if err != nil {
		return Summary{}, err
	}
	return Summary{
		Experiment: exp, MinTrades: c.MinTrades, MinTradesSource: c.MinTradesSource,
		BeforeTrades: c.Before.Performance.ClosedTradeCount, AfterTrades: c.After.Performance.ClosedTradeCount,
		Confounded: c.Confounded, Verdict: c.Verdict, Judged: c.Judged,
	}, nil
}

// SummarizeAll lists up to limit experiments (newest first, status "" for all)
// with their verdicts.
func SummarizeAll(ctx context.Context, q query.Q, status string, limit int, now time.Time, minTrades int, capitalJPY float64) ([]Summary, error) {
	exps, err := List(ctx, q, status, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(exps))
	for _, exp := range exps {
		s, err := Summarize(ctx, q, exp, now, minTrades, capitalJPY)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}
