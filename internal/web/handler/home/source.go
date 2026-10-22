package home

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Source is the Home screen's read-only access to the database-backed figures
// (issue #896). internal/bootstrap/homeview implements it over the
// application's database; every method is a SELECT-only read, so one failing
// read only takes down the card it feeds. now is the request's clock, so
// "today" (JST) is the same for every read of one render.
type Source interface {
	// Health is today's health (the Paper results MCP's health_today).
	Health(ctx context.Context, now time.Time) (Health, error)
	// RealizedToday is the Paper P&L of the positions closed since 00:00 JST
	// (the paper_daily_pnl figure of today).
	RealizedToday(ctx context.Context, now time.Time) (Realized, error)
	// RejectReasons counts today's NONE signals by reject_reason head, most
	// frequent first.
	RejectReasons(ctx context.Context, now time.Time) ([]RejectCount, error)
	// Proposals is today's order proposals: the count per status and the newest few.
	Proposals(ctx context.Context, now time.Time) (Proposals, error)
	// RecentTrades is the newest closed trades, at most limit.
	RecentTrades(ctx context.Context, now time.Time, limit int) ([]Trade, error)
	// OpenPositions is every open position (closed_at IS NULL), whatever its
	// age: the set the daily guard counts. It is not "the newest N rows", so
	// an old open position is never cut off by newer closed ones.
	OpenPositions(ctx context.Context) ([]domain.Position, error)
}

// Health is the part of health_today the Home screen shows.
type Health struct {
	// AttentionCodes are the fixed codes health_today raises (query.Attention*),
	// in its order.
	AttentionCodes []string
	// OpenedToday is how many Paper positions were opened today (JST).
	OpenedToday int
	// MetaAdopted is whether a meta-labeling model is adopted; MetaVersion and
	// MetaTrainedAt describe it (zero when unknown).
	MetaAdopted   bool
	MetaVersion   string
	MetaTrainedAt time.Time
	// BootstrapEnabled is the Settings switch paper.bootstrap_entry_enabled.
	BootstrapEnabled bool
	// RetrainSkipped is whether the newest nightly retraining was skipped and
	// no model was trained since; RetrainSkipReason is its fixed reason code.
	RetrainSkipped    bool
	RetrainSkipReason string
	RetrainSkippedAt  time.Time
	// StalledProposals counts order proposals stuck in received/processing.
	StalledProposals int
	// MissingSessionDates are recent trading days with no market snapshot.
	MissingSessionDates []string
}

// Realized is today's realized Paper P&L.
type Realized struct {
	PnLJPY      float64
	ClosedCount int
}

// RejectCount is how many NONE signals carry one reject_reason head.
type RejectCount struct {
	Reason string
	Count  int
}

// Proposals is today's order-proposal activity.
type Proposals struct {
	Total    int
	ByStatus []StatusCount
	Latest   []ProposalItem
}

// StatusCount is the proposals of one status (orderproposal.Status*).
type StatusCount struct {
	Status string
	Count  int
}

// ProposalItem is one proposal in brief.
type ProposalItem struct {
	ID         int64
	Symbol     string
	Action     string
	Status     string
	ReceivedAt time.Time
}

// Trade is one closed Paper trade.
type Trade struct {
	Symbol     string
	Side       string
	Quantity   int64
	ClosedAt   time.Time
	PnLJPY     float64
	ExitReason string
}

// StaticSource is the idle Source (no signal, no trade, a model adopted),
// internal/router.New()'s default until a real one is wired in.
type StaticSource struct{}

// Health implements Source.
func (StaticSource) Health(context.Context, time.Time) (Health, error) {
	return Health{MetaAdopted: true}, nil
}

// RealizedToday implements Source.
func (StaticSource) RealizedToday(context.Context, time.Time) (Realized, error) {
	return Realized{}, nil
}

// RejectReasons implements Source.
func (StaticSource) RejectReasons(context.Context, time.Time) ([]RejectCount, error) {
	return nil, nil
}

// Proposals implements Source.
func (StaticSource) Proposals(context.Context, time.Time) (Proposals, error) {
	return Proposals{}, nil
}

// RecentTrades implements Source.
func (StaticSource) RecentTrades(context.Context, time.Time, int) ([]Trade, error) {
	return nil, nil
}

// OpenPositions implements Source.
func (StaticSource) OpenPositions(context.Context) ([]domain.Position, error) {
	return nil, nil
}
