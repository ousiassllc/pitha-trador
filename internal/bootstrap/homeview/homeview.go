// Package homeview is the Home screen's database-backed source (issue #896): it
// implements internal/web/handler/home.Source over the application's database
// with the read models the Paper results MCP's tools use
// (internal/service/paperresultsmcp/query and .../proposal, called here and not
// modified), so the screen and those tools show the same values - the P&L of
// today is the figure paper_daily_pnl reports, the health is health_today's.
// Every read runs in one read-only transaction and only issues SELECT
// statements.
package homeview

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/rejectstats"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/home"
)

// latestProposals is how many of today's proposals Proposals lists.
const latestProposals = 3

// Reader implements home.Source over DB.
type Reader struct {
	DB *sql.DB
}

// today is [00:00 JST, next 00:00 JST) of the day containing now: the period
// of every "today" figure.
func today(now time.Time) query.Range {
	j := now.In(query.JST)
	start := time.Date(j.Year(), j.Month(), j.Day(), 0, 0, 0, 0, query.JST)
	return query.Range{From: start, To: start.AddDate(0, 0, 1)}
}

// readTx runs fn in one read-only transaction, so every query of a read sees
// one snapshot. The transaction is only ever rolled back.
func (r Reader) readTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return fmt.Errorf("homeview: begin read-only transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	return fn(tx)
}

// Health returns health_today's data, reduced to what the screen shows.
func (r Reader) Health(ctx context.Context, now time.Time) (home.Health, error) {
	var out home.Health
	err := r.readTx(ctx, func(tx *sql.Tx) error {
		h, err := query.HealthToday(ctx, tx, now)
		if err != nil {
			return err
		}
		out = home.Health{
			AttentionCodes:      h.AttentionCodes,
			OpenedToday:         h.PaperTrades.OpenedCount,
			MetaAdopted:         h.MetaModel.Adopted,
			MetaVersion:         deref(h.MetaModel.Version),
			MetaTrainedAt:       parseOut(h.MetaModel.TrainedAt),
			BootstrapEnabled:    h.Bootstrap.Enabled,
			RetrainSkipped:      h.Retrain.Skipped,
			RetrainSkipReason:   deref(h.Retrain.ReasonCode),
			RetrainSkippedAt:    parseOut(h.Retrain.LatestSkippedAt),
			StalledProposals:    h.OrderProposals.Count,
			MissingSessionDates: h.MissingSessionDates,
		}
		return nil
	})
	return out, err
}

// RealizedToday is today's row of paper_daily_pnl: the realized P&L and the
// number of Paper positions closed since 00:00 JST (zero without a close).
func (r Reader) RealizedToday(ctx context.Context, now time.Time) (home.Realized, error) {
	var out home.Realized
	err := r.readTx(ctx, func(tx *sql.Tx) error {
		days, err := query.DailyPnL(ctx, tx, today(now))
		if err != nil {
			return err
		}
		for _, d := range days {
			out.PnLJPY += d.PnlJPY
			out.ClosedCount += d.TradeCount
		}
		return nil
	})
	return out, err
}

// RejectReasons counts today's NONE signals by reject_reason head.
func (r Reader) RejectReasons(ctx context.Context, now time.Time) ([]home.RejectCount, error) {
	var out []home.RejectCount
	err := r.readTx(ctx, func(tx *sql.Tx) error {
		p := today(now)
		counts, err := rejectstats.TodayCounts(ctx, tx, p.From, p.To)
		for _, c := range counts {
			out = append(out, home.RejectCount{Reason: c.Reason, Count: c.Count})
		}
		return err
	})
	return out, err
}

// Proposals is order_proposal_summary's counts and order_proposals_list's
// newest rows for today.
func (r Reader) Proposals(ctx context.Context, now time.Time) (home.Proposals, error) {
	var out home.Proposals
	err := r.readTx(ctx, func(tx *sql.Tx) error {
		p := today(now)
		sum, err := proposal.Summarize(ctx, tx, p)
		if err != nil {
			return err
		}
		items, _, err := proposal.List(ctx, tx, proposal.ListFilter{Range: p, Limit: latestProposals})
		if err != nil {
			return err
		}
		out.Total = sum.TotalCount
		for _, c := range sum.ByStatus {
			out.ByStatus = append(out.ByStatus, home.StatusCount{Status: c.Code, Count: c.Count})
		}
		for _, it := range items {
			out.Latest = append(out.Latest, home.ProposalItem{
				ID: it.ProposalID, Symbol: it.Symbol, Action: it.Action, Status: it.Status, ReceivedAt: parseOut(&it.ReceivedAt),
			})
		}
		return nil
	})
	return out, err
}

// RecentTrades is paper_trades' newest rows: the closed Paper trades, newest
// first.
func (r Reader) RecentTrades(ctx context.Context, now time.Time, limit int) ([]home.Trade, error) {
	var out []home.Trade
	err := r.readTx(ctx, func(tx *sql.Tx) error {
		// closed_at is never in the future; the extra day only guards a clock
		// behind the stored timestamps.
		trades, _, err := query.Trades(ctx, tx, query.Range{From: time.Unix(0, 0), To: now.Add(24 * time.Hour)}, limit, "")
		for _, t := range trades {
			out = append(out, home.Trade{
				Symbol: t.Symbol, Side: t.Side, Quantity: t.Quantity, ClosedAt: parseOut(&t.ClosedAt),
				PnLJPY: t.RealizedPnl, ExitReason: deref(t.ExitReason),
			})
		}
		return err
	})
	return out, err
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// parseOut reads a timestamp the query package rendered (UTC RFC3339); zero
// when absent or not a timestamp.
func parseOut(s *string) time.Time {
	if s == nil {
		return time.Time{}
	}
	t, err := sqlutil.ParseTime(*s)
	if err != nil {
		return time.Time{}
	}
	return t
}

var _ home.Source = Reader{}
