package featureengine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// CycleInput is one instrument's data for a single Feature Engine scan
// cycle (functional.md §4.1, overview.md §4 "Feature Engine").
type CycleInput struct {
	InstrumentID int64
	Symbol       string
	Input        Input
	// RawDataJSON is the kabuステーションAPI response this bar's Reading
	// was derived from, kept verbatim for re-calculation/audit
	// (er.md §market_snapshots raw_data_json).
	RawDataJSON string
}

// Engine computes Feature values for a full scan cycle's worth of
// instruments and persists them to market_snapshots.
type Engine struct {
	snapshots *market.SnapshotRepository
	rag       *rag.Service
}

// NewEngine returns an Engine that persists computed snapshots via
// snapshots, and - once each cycle's snapshots are committed - indexes
// their standardized feature embedding into market_snapshot_vectors via
// ragService for RAG similarity search (functional.md FR-RAG-1,
// docs/architecture/overview.md §7).
func NewEngine(snapshots *market.SnapshotRepository, ragService *rag.Service) *Engine {
	return &Engine{snapshots: snapshots, rag: ragService}
}

// RunCycle computes Compute(in.Input) for every element of inputs and
// persists the resulting market_snapshots rows in a single transaction
// (er.md §market_snapshots "運用上の注意": 周期実行1サイクル分をまとめて
// 1トランザクションで書き込む). SnapshotRepository.InsertBatch already
// provides the all-or-nothing transaction, so a duplicate-bar violation
// on one instrument rolls back the whole cycle rather than leaving it
// partially written.
func (e *Engine) RunCycle(ctx context.Context, inputs []CycleInput) ([]domain.Snapshot, error) {
	built := make([]domain.Snapshot, 0, len(inputs))
	for _, in := range inputs {
		built = append(built, domain.Snapshot{
			InstrumentID: in.InstrumentID,
			Symbol:       in.Symbol,
			Timestamp:    in.Input.Timestamp,
			Price:        in.Input.Current.Price,
			Bid:          in.Input.Current.Bid,
			Ask:          in.Input.Current.Ask,
			SpreadBps:    spreadBps(in.Input.Current.Bid, in.Input.Current.Ask),
			Volume:       in.Input.Current.Volume,
			Turnover:     in.Input.Current.Turnover,
			Feature:      Compute(in.Input),
			SpecialQuote: in.Input.Current.SpecialQuote,
			PriceLimit:   in.Input.Current.PriceLimit,
			Lendable:     in.Input.Current.Lendable,
			RawDataJSON:  in.RawDataJSON,
		})
	}

	saved, err := e.snapshots.InsertBatch(ctx, built)
	if err != nil {
		return nil, fmt.Errorf("featureengine: persist cycle snapshots: %w", err)
	}

	// RAG indexing is a best-effort enrichment (FR-RAG-1): a failure here
	// must never undo or block the already-committed market_snapshots
	// rows RunCycle's callers (Fast Screener, Jev Scout) depend on.
	for _, snap := range saved {
		in := rag.FeatureInputFromFeature(snap.Feature, snap.SpreadBps)
		if err := e.rag.IndexSnapshot(ctx, snap.ID, in); err != nil {
			slog.Error("featureengine: index snapshot vector failed", "snapshot_id", snap.ID, "symbol", snap.Symbol, "error", err)
		}
	}

	return saved, nil
}

// spreadBps computes (ask-bid)/midprice*10000, or nil if either quote is
// missing (FR-FE-2), the midprice is zero, or the book is crossed
// (bid > ask: special quote, around the open/close or one stale side).
// A crossed book would yield a negative spread that slips past the
// upper-bound-only spread guards (screener/policy/risk), so it is treated
// as unavailable and those guards fail closed (missing_spread).
func spreadBps(bid, ask *float64) *float64 {
	if bid == nil || ask == nil || *bid > *ask {
		return nil
	}
	mid := (*bid + *ask) / 2
	if mid == 0 {
		return nil
	}
	v := (*ask - *bid) / mid * 10000
	return &v
}
