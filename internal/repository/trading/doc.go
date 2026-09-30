// Package trading persists the paper-trading lifecycle: trade_signals
// (SignalRepository, signal_repo.go), paper_orders (OrderRepository,
// order_repo.go) and positions (PositionRepository, position_repo.go).
// OrderRepository.FillEntry and PositionRepository.CloseWithExitOrder write
// the order and position rows in one transaction, which is why the three
// tables live in one package.
//
// It MUST depend only on internal/domain and internal/repository/sqlutil.
// See docs/architecture/overview.md §3.
package trading
