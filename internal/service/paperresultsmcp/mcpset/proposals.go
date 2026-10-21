package mcpset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// The order-proposal side of the Writer (issue #827). Its whole write surface
// is two transactions: SubmitProposal (one order_proposals row plus its first
// order_proposal_events row) and DisableOrderProposals (the switch off plus
// its audit row). Nothing here touches paper_orders, positions, trade_signals
// or kill_switch_events.

var (
	// ErrProposalsDisabled is returned while the order-proposal switch is off
	// (nothing stored, 0, or a damaged value). Nothing was stored.
	ErrProposalsDisabled = errors.New("mcpset: order proposals are switched off")
	// ErrProposalLimit is returned when MaxPerDay proposals were already
	// accepted on the JST day. Nothing was stored.
	ErrProposalLimit = errors.New("mcpset: daily order proposal limit reached")
	// ErrIdempotencyConflict is returned for an idempotency_key already used
	// by a proposal with different content. Nothing was stored.
	ErrIdempotencyConflict = errors.New("mcpset: idempotency_key was used with different content")
)

// OrderProposalsEnabled reports the order-proposal switch: the stored 0/1,
// and OFF while nothing is stored or the stored value is damaged (fail
// closed - the opposite default of the write switch).
func OrderProposalsEnabled(ctx context.Context, store interface {
	Get(ctx context.Context, key string) (string, bool, error)
}) (bool, error) {
	raw, ok, err := store.Get(ctx, mcpwritesettings.KeyOrderProposalsEnabled)
	if err != nil {
		return false, err
	}
	if !ok {
		return mcpwritesettings.DefaultOrderProposalsEnabled, nil
	}
	on, err := mcpwritesettings.ApplyOrderProposals(raw)
	return on && err == nil, nil
}

// Submitted is the outcome of SubmitProposal: the proposal's id and state,
// and whether the call stored a new row (Replayed false) or recognized a
// re-sent proposal and returned the first one (Replayed true).
type Submitted struct {
	ProposalID int64  `json:"proposal_id"`
	Status     string `json:"status"`
	ReceivedAt string `json:"received_at"`
	ExpiresAt  string `json:"expires_at"`
	Replayed   bool   `json:"replayed"`
}

// SubmitProposal accepts req and records it, in one transaction: the order
// is the switch (ErrProposalsDisabled), the JST-day limit (ErrProposalLimit),
// the idempotency check (a same-content re-send returns the first proposal,
// different content is ErrIdempotencyConflict), then the proposal row and its
// first event (received). Each check runs before anything is stored. client
// is the caller's host.
func (w *Writer) SubmitProposal(ctx context.Context, client string, req proposal.Request) (Submitted, error) {
	var out Submitted
	err := runInTx(ctx, w.db, "proposal transaction", func(tx *sql.Tx) error {
		on, err := OrderProposalsEnabled(ctx, system.NewRuntimeSettingsRepositoryOn(tx))
		if err != nil {
			return err
		}
		if !on {
			return ErrProposalsDisabled
		}
		now := w.now()
		n, err := countReceivedOn(ctx, tx, now)
		if err != nil {
			return err
		}
		if n >= proposal.MaxPerDay {
			return ErrProposalLimit
		}
		if prev, found, err := findByKey(ctx, tx, req.IdempotencyKey); err != nil {
			return err
		} else if found {
			if prev.hash != req.Hash() {
				return ErrIdempotencyConflict
			}
			out = Submitted{ProposalID: prev.id, Status: prev.status, ReceivedAt: prev.receivedAt, ExpiresAt: prev.expiresAt, Replayed: true}
			return nil
		}
		out, err = insertProposal(ctx, tx, now, client, req)
		return err
	})
	return out, err
}

// countReceivedOn counts the proposals accepted on the JST day of now.
func countReceivedOn(ctx context.Context, tx *sql.Tx, now time.Time) (int, error) {
	day := now.In(query.JST)
	from := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, query.JST)
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_proposals WHERE received_at >= ? AND received_at < ?`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(from.AddDate(0, 0, 1))).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("mcpset: count today's proposals: %w", err)
	}
	return n, nil
}

type storedProposal struct {
	id                                  int64
	hash, status, receivedAt, expiresAt string
}

func findByKey(ctx context.Context, tx *sql.Tx, key string) (storedProposal, bool, error) {
	var p storedProposal
	err := tx.QueryRowContext(ctx, `SELECT id, request_hash, status, received_at, expires_at FROM order_proposals WHERE idempotency_key = ?`, key).
		Scan(&p.id, &p.hash, &p.status, &p.receivedAt, &p.expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return storedProposal{}, false, nil
	}
	if err != nil {
		return storedProposal{}, false, fmt.Errorf("mcpset: find proposal by idempotency key: %w", err)
	}
	p.receivedAt, p.expiresAt = query.FormatOut(p.receivedAt), query.FormatOut(p.expiresAt)
	return p, true, nil
}

// insertProposal is the one INSERT pair of a new proposal: its row (status
// received, the application's link columns NULL) and its first event.
func insertProposal(ctx context.Context, tx *sql.Tx, now time.Time, client string, req proposal.Request) (Submitted, error) {
	at := sqlutil.FormatTime(now)
	res, err := tx.ExecContext(ctx, `
INSERT INTO order_proposals (
    idempotency_key, request_hash, received_at, client, mode, symbol, bucket, action, quantity, budget_jpy,
    order_type, limit_price, stop_loss_price, take_profit_price, exit_plan, max_loss_jpy, rationale, sources_json,
    expires_at, status, status_reason, updated_at)
VALUES (?, ?, ?, ?, 'paper', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?)`,
		req.IdempotencyKey, req.Hash(), at, client, req.Symbol, req.Bucket, req.Action, req.Quantity, req.BudgetJPY,
		req.OrderType, req.LimitPrice, req.StopLossPrice, req.TakeProfitPrice, req.ExitPlan, req.MaxLossJPY, req.Rationale, req.SourcesJSON(),
		sqlutil.FormatTime(req.ExpiresAt), proposal.StatusReceived, at)
	if err != nil {
		return Submitted{}, fmt.Errorf("mcpset: insert order proposal: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Submitted{}, fmt.Errorf("mcpset: order proposal id: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor)
VALUES (?, ?, NULL, ?, '', ?)`, id, at, proposal.StatusReceived, proposal.ActorMCP); err != nil {
		return Submitted{}, fmt.Errorf("mcpset: insert order proposal event: %w", err)
	}
	return Submitted{
		ProposalID: id, Status: proposal.StatusReceived,
		ReceivedAt: now.UTC().Format(time.RFC3339), ExpiresAt: req.ExpiresAt.UTC().Format(time.RFC3339),
	}, nil
}

// DisableOrderProposals turns the order-proposal switch off and records it,
// in one transaction. It is the MCP's only way to change the switch: nothing
// here turns it on (that is the Settings screen's Gate.SetOrderProposalsEnabled).
func (w *Writer) DisableOrderProposals(ctx context.Context, client string) (Entry, error) {
	var out Entry
	err := runInTx(ctx, w.db, "proposal switch transaction", func(tx *sql.Tx) error {
		store := system.NewRuntimeSettingsRepositoryOn(tx)
		was, err := OrderProposalsEnabled(ctx, store)
		if err != nil {
			return err
		}
		now := w.now()
		if err := store.Set(ctx, mcpwritesettings.KeyOrderProposalsEnabled, mcpwritesettings.Text(false), now); err != nil {
			return err
		}
		out, err = insertAudit(ctx, tx, now, proposalSwitchEntry(ActorMCP, client, was, false))
		return err
	})
	return out, err
}

// proposalSwitchEntry is the audit row of an order-proposal switch change.
func proposalSwitchEntry(actor, client string, was, now bool) Entry {
	action := ActionOrderProposalsDisable
	if now {
		action = ActionOrderProposalsEnable
	}
	return Entry{
		Actor: actor, Client: client, Action: action, Key: mcpwritesettings.KeyOrderProposalsEnabled,
		Before: new(mcpwritesettings.Text(was)), After: new(mcpwritesettings.Text(now)),
	}
}
