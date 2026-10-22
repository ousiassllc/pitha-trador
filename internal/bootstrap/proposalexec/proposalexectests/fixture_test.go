package proposalexectests

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/proposalexec"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
	"github.com/ousiassllc/pitha-trador/internal/repository/proposalstore"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
)

// at is a JST time on 2026-09-29 (Tue), a trading day.
func at(hour, minute, second int) time.Time {
	return time.Date(2026, 9, 29, hour, minute, second, 0, marketcalendar.JST)
}

// fixture is the real Paper wiring (Risk Engine, Execution Engine, the 000043
// schema) with the roomy 30M-yen limits (2% per symbol: 600,000 yen) and a
// clock the test moves.
type fixture struct {
	t     *testing.T
	svc   *bootstrap.Services
	db    *sql.DB
	inst  domain.Instrument
	exec  *spyExecution
	risk  *spyRisk
	props *proposalstore.Repository
	proc  proposalexec.Processor
	clock *time.Time
	keys  int
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	state.Risk.Paper.InitialCapital, state.Risk.Paper.MaxPositionPerSymbolPct, state.Risk.Paper.MaxTotalExposurePct = 30_000_000, 2, 20
	f := &fixture{t: t, db: state.DB, clock: new(time.Time)}
	*f.clock = at(10, 0, 30)
	f.svc = bootstrap.BuildServices(state, config.Secrets{KabuAPIPassword: "test-password", JevBaseURL: "http://127.0.0.1:1"},
		bootstrap.WithExecutionClock(func() time.Time { return *f.clock }))
	if f.inst, err = f.svc.Instruments.Create(context.Background(), domain.Instrument{Symbol: "7203", Name: "Toyota", Market: "TSE Prime", IsActive: true}); err != nil {
		t.Fatalf("create instrument: %v", err)
	}
	f.exec = &spyExecution{Engine: f.svc.Execution}
	f.risk = &spyRisk{Risk: f.svc.Risk}
	f.props = proposalstore.New(state.DB, func() time.Time { return *f.clock })
	f.proc = proposalexec.Processor{
		Proposals: f.props, Instruments: f.svc.Instruments, Snapshots: f.svc.Snapshots, Signals: f.svc.Signals,
		Orders: f.svc.Orders, Positions: f.svc.Positions, Risk: f.risk, Execution: f.exec,
		SnapshotMaxAge: 5 * time.Minute,
	}
	f.quote(2500)
	return f
}

// quote stores the latest bar of the instrument: price, no spread, stamped at 10:00:00.
func (f *fixture) quote(price float64) {
	f.t.Helper()
	if _, err := f.svc.Snapshots.Insert(context.Background(), domain.Snapshot{
		InstrumentID: f.inst.ID, Symbol: f.inst.Symbol, Timestamp: at(10, 0, 0), Price: price, SpreadBps: new(float64), RawDataJSON: "{}",
	}); err != nil {
		f.t.Fatalf("insert snapshot: %v", err)
	}
}

// spec is a proposal as the MCP records it. Zero fields take the defaults of
// a valid open_long: 200 shares of 7203 at market, stop 2400, expiring in an hour.
type spec struct {
	symbol, bucket, action, orderType string
	quantity                          int64
	budget, limit, stop               float64
	expires                           time.Time
}

// propose inserts one received proposal the way mcpset.Writer.SubmitProposal
// does and returns its id.
func (f *fixture) propose(s spec) int64 {
	f.t.Helper()
	if s.symbol == "" {
		s.symbol = "7203"
	}
	if s.bucket == "" {
		s.bucket = domain.BucketStock
	}
	if s.action == "" {
		s.action = orderproposal.ActionOpenLong
	}
	if s.orderType == "" {
		s.orderType = domain.OrderTypeMarket
	}
	if s.quantity == 0 && s.budget == 0 && s.action == orderproposal.ActionOpenLong {
		s.quantity = 200
	}
	if s.stop == 0 && s.action == orderproposal.ActionOpenLong {
		s.stop = 2400
		if s.limit != 0 && s.limit <= s.stop { // the MCP refuses a stop at or above the limit
			s.stop = s.limit - 100
		}
	}
	if s.expires.IsZero() {
		s.expires = at(11, 0, 0)
	}
	f.keys++
	nullable := func(v float64) any {
		if v == 0 {
			return nil
		}
		return v
	}
	qty := any(nil)
	if s.quantity != 0 {
		qty = s.quantity
	}
	received := sqlutil.FormatTime(at(9, 30, 0))
	res, err := f.db.Exec(`
INSERT INTO order_proposals (idempotency_key, request_hash, received_at, mode, symbol, bucket, action, quantity, budget_jpy, order_type,
    limit_price, stop_loss_price, max_loss_jpy, rationale, sources_json, expires_at, status, status_reason, updated_at)
VALUES (?, 'h', ?, 'paper', ?, ?, ?, ?, ?, ?, ?, ?, ?, 'test', '[]', ?, 'received', '', ?)`,
		fmt.Sprintf("proposal-%04d", f.keys), received, s.symbol, s.bucket, s.action, qty, nullable(s.budget), s.orderType,
		nullable(s.limit), nullable(s.stop), 100000.0, sqlutil.FormatTime(s.expires), received)
	if err != nil {
		f.t.Fatalf("insert proposal: %v", err)
	}
	id, _ := res.LastInsertId()
	if _, err := f.db.Exec(`INSERT INTO order_proposal_events (proposal_id, at, from_status, to_status, reason_code, actor)
VALUES (?, ?, NULL, 'received', '', 'mcp')`, id, received); err != nil {
		f.t.Fatalf("insert event: %v", err)
	}
	return id
}

// cycle runs one processing pass.
func (f *fixture) cycle() {
	f.t.Helper()
	if _, err := f.proc.Cycle(context.Background()); err != nil {
		f.t.Fatalf("Cycle: %v", err)
	}
}

// get reads the proposal back.
func (f *fixture) get(id int64) orderproposal.Proposal {
	f.t.Helper()
	p, err := f.props.Get(context.Background(), id)
	if err != nil {
		f.t.Fatalf("Get(%d): %v", id, err)
	}
	return p
}

// wantEnd fails unless the proposal ended in status with reason.
func (f *fixture) wantEnd(id int64, status, reason string) orderproposal.Proposal {
	f.t.Helper()
	p := f.get(id)
	if p.Status != status || p.StatusReason != reason {
		f.t.Fatalf("proposal %d = (%s, %q), want (%s, %q)", id, p.Status, p.StatusReason, status, reason)
	}
	return p
}

// count is SELECT COUNT(*) for a table (with an optional WHERE).
func (f *fixture) count(table, where string) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM ` + table + ` ` + where).Scan(&n); err != nil {
		f.t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// events returns "from>to:reason" of every event of the proposal, oldest first.
func (f *fixture) events(id int64) []string {
	f.t.Helper()
	rows, err := f.db.Query(`SELECT COALESCE(from_status, ''), to_status, reason_code, detail_json FROM order_proposal_events WHERE proposal_id = ? ORDER BY id`, id)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var from, to, reason, detail string
		if err := rows.Scan(&from, &to, &reason, &detail); err != nil {
			f.t.Fatal(err)
		}
		e := from + ">" + to
		if reason != "" {
			e += ":" + reason
		}
		if detail != "" {
			e += " " + detail
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

// openPosition opens a LONG position an order proposal entered, marked at
// price: an open_long proposal (filled) is linked to it through position_id.
// It writes no trade_signals row; the signal path of the same judgement is
// openPositionFrom with orderproposal.PolicyVersion.
func (f *fixture) openPosition(quantity int64, price float64) domain.Position {
	f.t.Helper()
	pos := f.openPositionFrom("", quantity, price)
	id := f.propose(spec{quantity: quantity})
	if _, err := f.db.Exec(`UPDATE order_proposals SET status = 'filled', position_id = ?, instrument_id = ? WHERE id = ?`, pos.ID, f.inst.ID, id); err != nil {
		f.t.Fatalf("link the entry proposal: %v", err)
	}
	return pos
}

// openPositionFrom opens a LONG position on the instrument directly (as an
// earlier entry would have) whose entry order's signal carries policyVersion
// (no signal when it is empty), marked at price.
func (f *fixture) openPositionFrom(policyVersion string, quantity int64, price float64) domain.Position {
	f.t.Helper()
	ctx := context.Background()
	entry := domain.PaperOrder{
		InstrumentID: f.inst.ID, Symbol: f.inst.Symbol, Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket,
		Quantity: quantity, Status: domain.OrderStatusFilled, SubmittedAt: at(9, 40, 0),
	}
	if policyVersion != "" {
		sig, err := f.svc.Signals.Insert(ctx, domain.TradeSignal{
			InstrumentID: f.inst.ID, Symbol: f.inst.Symbol, Timestamp: at(9, 40, 0), Direction: domain.JevDirectionLong,
			PolicyVersion: policyVersion, RiskPassed: true, CreatedAt: at(9, 40, 0),
		})
		if err != nil {
			f.t.Fatalf("insert entry signal: %v", err)
		}
		entry.TradeSignalID = &sig.ID
	}
	order, err := f.svc.Orders.Insert(ctx, entry)
	if err != nil {
		f.t.Fatalf("insert entry order: %v", err)
	}
	pos, err := f.svc.Positions.Open(ctx, domain.Position{
		InstrumentID: f.inst.ID, EntryOrderID: order.ID, Symbol: f.inst.Symbol, Side: domain.PositionSideLong,
		Quantity: quantity, EntryPrice: price, CurrentPrice: price, OpenedAt: at(9, 40, 0),
	})
	if err != nil {
		f.t.Fatalf("open position: %v", err)
	}
	return pos
}

func (f *fixture) setActive(active bool) {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE instruments SET is_active = ? WHERE id = ?`, active, f.inst.ID); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) setKind(kind string) {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE instruments SET kind = ? WHERE id = ?`, kind, f.inst.ID); err != nil {
		f.t.Fatal(err)
	}
}

// fillBook is the unknown quote: a fill at the price without a spread.
func fillBook() fillmodel.Book { return fillmodel.Book{} }

// dump renders a query's rows as tab-separated lines, for the evidence logs.
func (f *fixture) dump(query string) string {
	f.t.Helper()
	rows, err := f.db.Query(query)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	cols, _ := rows.Columns()
	var out strings.Builder
	out.WriteString(strings.Join(cols, "\t") + "\n")
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			f.t.Fatal(err)
		}
		cells := make([]string, len(cols))
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			cells[i] = fmt.Sprint(v)
		}
		out.WriteString(strings.Join(cells, "\t") + "\n")
	}
	if err := rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out.String()
}

// requestCancel records a cancellation request the way mcpset.Writer.RequestCancel does.
func (f *fixture) requestCancel(id int64) {
	f.t.Helper()
	if _, err := f.db.Exec(`UPDATE order_proposals SET cancel_requested_at = ? WHERE id = ?`, sqlutil.FormatTime(*f.clock), id); err != nil {
		f.t.Fatal(err)
	}
}
