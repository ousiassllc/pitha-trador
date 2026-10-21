package proposalstore_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
)

func newRepo(t *testing.T) (*proposalstore.Repository, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO order_proposals (idempotency_key, request_hash, received_at, symbol, bucket, action, quantity, order_type,
		stop_loss_price, max_loss_jpy, rationale, sources_json, expires_at, updated_at)
		VALUES ('key-00000001', 'h', '2026-09-29T00:30:00Z', '7203', 'stock', 'open_long', 100, 'MARKET', 900, 1000, 'why', '[]', '2026-09-29T02:00:00Z', 't')`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)
	return proposalstore.New(db, func() time.Time { return now }), db
}

// A status change is guarded by the status it was read at: when another
// writer moved the proposal first, nothing is written, not even the event.
func TestTransitionIsGuardedByTheStatusItWasReadAt(t *testing.T) {
	repo, db := newRepo(t)
	ctx := context.Background()
	if err := repo.Transition(ctx, 1, orderproposal.StatusReceived, proposalstore.Change{ToStatus: orderproposal.StatusProcessing}); err != nil {
		t.Fatal(err)
	}
	err := repo.Transition(ctx, 1, orderproposal.StatusReceived, proposalstore.Change{ToStatus: orderproposal.StatusExpired, Reason: "expired"})
	if !errors.Is(err, proposalstore.ErrStatusChanged) {
		t.Fatalf("second transition from received = %v, want ErrStatusChanged", err)
	}
	p, _ := repo.Get(ctx, 1)
	if p.Status != orderproposal.StatusProcessing || p.StatusReason != "" || countOf(t, db, `SELECT COUNT(*) FROM order_proposal_events`) != 1 {
		t.Fatalf("proposal = (%s, %q), events = %d; want processing, no reason, one event", p.Status, p.StatusReason, countOf(t, db, `SELECT COUNT(*) FROM order_proposal_events`))
	}
}

// A transition with no new status is an event on the same status (a quantity
// reduction); links not set stay as they were.
func TestTransitionInPlaceRecordsAnEventAndKeepsEarlierLinks(t *testing.T) {
	repo, db := newRepo(t)
	ctx := context.Background()
	if err := repo.Link(ctx, 1, proposalstore.Links{ComputedMaxLossJPY: new(500.0)}); err != nil {
		t.Fatal(err)
	}
	applied := int64(100)
	err := repo.Transition(ctx, 1, orderproposal.StatusReceived, proposalstore.Change{
		Reason: "quantity_reduced", DetailJSON: `{"requested_quantity":200,"applied_quantity":100}`, Links: proposalstore.Links{AppliedQuantity: &applied}})
	if err != nil {
		t.Fatal(err)
	}
	p, _ := repo.Get(ctx, 1)
	if p.Status != orderproposal.StatusReceived || p.StatusReason != "quantity_reduced" {
		t.Fatalf("proposal = (%s, %q), want the same status with the reason", p.Status, p.StatusReason)
	}
	var loss float64
	if err := db.QueryRow(`SELECT computed_max_loss_jpy FROM order_proposals`).Scan(&loss); err != nil || loss != 500 {
		t.Fatalf("computed_max_loss_jpy = %v (%v), want the earlier link kept", loss, err)
	}
	if n := countOf(t, db, `SELECT COUNT(*) FROM order_proposal_events WHERE from_status = to_status AND detail_json <> ''`); n != 1 {
		t.Fatalf("in-place events = %d, want 1", n)
	}
}

func TestActiveListsReceivedWaitingAndSubmittedOldestFirst(t *testing.T) {
	repo, _ := newRepo(t)
	ctx := context.Background()
	active, err := repo.Active(ctx)
	if err != nil || len(active) != 1 || active[0].Symbol != "7203" || active[0].StopLossPrice == nil || *active[0].StopLossPrice != 900 {
		t.Fatalf("Active = %+v, %v; want the one received proposal with its stop", active, err)
	}
	if err := repo.Transition(ctx, 1, orderproposal.StatusReceived, proposalstore.Change{ToStatus: orderproposal.StatusProcessing}); err != nil {
		t.Fatal(err)
	}
	if active, _ = repo.Active(ctx); len(active) != 0 {
		t.Fatalf("Active while processing = %d, want 0", len(active))
	}
	if stuck, _ := repo.Processing(ctx); len(stuck) != 1 {
		t.Fatalf("Processing = %d, want 1", len(stuck))
	}
}

func countOf(t *testing.T, db *sql.DB, q string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
