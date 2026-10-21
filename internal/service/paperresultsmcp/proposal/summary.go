package proposal

import (
	"context"
	"fmt"
	"sort"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Count is a code and how many proposals carry it.
type Count struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// Summary is order_proposal_summary's data: the proposals received in the
// period, by status, by rejection reason and by bucket, and how the filled
// ones turned out.
type Summary struct {
	TotalCount int `json:"total_count"`
	// ByStatus lists every status code in orderproposal.Statuses() order, zero
	// counts included. ByRejectReason is the status_reason of the rejected
	// proposals (fixed codes, most frequent first). ByBucket lists stock and
	// bond_etf.
	ByStatus       []Count `json:"by_status"`
	ByRejectReason []Count `json:"by_reject_reason"`
	ByBucket       []Count `json:"by_bucket"`
	Filled         Filled  `json:"filled"`
}

// Filled is how the open_long proposals that were filled (the application
// linked a position to them) turned out. A close proposal is not counted: it
// closes a position some other proposal or rule opened, so counting its pnl
// again would double it. Pnl is positions.realized_pnl as stored; win_rate and
// expectancy are over the closed ones and nil while none has closed.
type Filled struct {
	ProposalCount int      `json:"proposal_count"`
	OpenCount     int      `json:"open_count"`
	ClosedCount   int      `json:"closed_count"`
	WinCount      int      `json:"win_count"`
	LossCount     int      `json:"loss_count"`
	WinRate       *float64 `json:"win_rate"`
	TotalPnlJPY   float64  `json:"total_pnl_jpy"`
	ExpectancyJPY *float64 `json:"expectancy_jpy"`
}

var buckets = []string{BucketStock, BucketBondETF}

// Summarize aggregates the proposals received in r.
func Summarize(ctx context.Context, q query.Q, r query.Range) (Summary, error) {
	from, to := r.Bounds()
	s := Summary{ByRejectReason: []Count{}}
	var err error
	if s.ByStatus, err = counts(ctx, q, "status", orderproposal.Statuses(), "", from, to); err != nil {
		return Summary{}, err
	}
	if s.ByBucket, err = counts(ctx, q, "bucket", buckets, "", from, to); err != nil {
		return Summary{}, err
	}
	for _, c := range s.ByStatus {
		s.TotalCount += c.Count
	}
	reasons, err := counts(ctx, q, "status_reason", nil, "status = 'rejected'", from, to)
	if err != nil {
		return Summary{}, err
	}
	for _, c := range reasons {
		if c.Code != "" {
			s.ByRejectReason = append(s.ByRejectReason, c)
		}
	}
	if s.Filled, err = filled(ctx, q, from, to); err != nil {
		return Summary{}, err
	}
	return s, nil
}

// counts groups the proposals received in [from, to) by column (a fixed name
// from this file, never client input). fixed lists the codes to report even
// at zero, in order; with none, the codes found are returned most frequent
// first. extra is an additional fixed condition.
func counts(ctx context.Context, q query.Q, column string, fixed []string, extra, from, to string) ([]Count, error) {
	where := "received_at >= ? AND received_at < ?"
	if extra != "" {
		where += " AND " + extra
	}
	rows, err := q.QueryContext(ctx, `SELECT `+column+`, COUNT(*) FROM order_proposals WHERE `+where+` GROUP BY `+column, from, to)
	if err != nil {
		return nil, fmt.Errorf("proposal: count order proposals by %s: %w", column, err)
	}
	defer func() { _ = rows.Close() }()
	found := map[string]int{}
	for rows.Next() {
		var (
			code string
			n    int
		)
		if err := rows.Scan(&code, &n); err != nil {
			return nil, fmt.Errorf("proposal: scan %s count: %w", column, err)
		}
		found[code] = n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("proposal: count order proposals by %s: %w", column, err)
	}
	out := []Count{}
	if fixed == nil {
		for code, n := range found {
			out = append(out, Count{Code: code, Count: n})
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count != out[j].Count {
				return out[i].Count > out[j].Count
			}
			return out[i].Code < out[j].Code
		})
		return out, nil
	}
	for _, code := range fixed {
		out = append(out, Count{Code: code, Count: found[code]})
	}
	return out, nil
}

func filled(ctx context.Context, q query.Q, from, to string) (Filled, error) {
	var f Filled
	err := q.QueryRowContext(ctx, `
SELECT COUNT(*),
       COALESCE(SUM(p.closed_at IS NULL), 0),
       COALESCE(SUM(p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL), 0),
       COALESCE(SUM(p.closed_at IS NOT NULL AND p.realized_pnl > 0), 0),
       COALESCE(SUM(p.closed_at IS NOT NULL AND p.realized_pnl < 0), 0),
       COALESCE(SUM(CASE WHEN p.closed_at IS NOT NULL THEN p.realized_pnl END), 0)
FROM order_proposals o
JOIN positions p ON p.id = o.position_id
WHERE o.action = 'open_long' AND o.received_at >= ? AND o.received_at < ?`, from, to).
		Scan(&f.ProposalCount, &f.OpenCount, &f.ClosedCount, &f.WinCount, &f.LossCount, &f.TotalPnlJPY)
	if err != nil {
		return Filled{}, fmt.Errorf("proposal: filled order proposals: %w", err)
	}
	if f.ClosedCount > 0 {
		rate := float64(f.WinCount) / float64(f.ClosedCount)
		mean := f.TotalPnlJPY / float64(f.ClosedCount)
		f.WinRate, f.ExpectancyJPY = &rate, &mean
	}
	return f, nil
}
