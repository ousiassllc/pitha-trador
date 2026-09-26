package featureengine

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository"
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
	snapshots *repository.SnapshotRepository
}

// NewEngine returns an Engine that persists computed snapshots via
// snapshots.
func NewEngine(snapshots *repository.SnapshotRepository) *Engine {
	return &Engine{snapshots: snapshots}
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
			RawDataJSON:  in.RawDataJSON,
		})
	}

	saved, err := e.snapshots.InsertBatch(ctx, built)
	if err != nil {
		return nil, fmt.Errorf("featureengine: persist cycle snapshots: %w", err)
	}
	return saved, nil
}

// spreadBps computes (ask-bid)/midprice*10000, or nil if either quote is
// missing (FR-FE-2) or the midprice is zero.
func spreadBps(bid, ask *float64) *float64 {
	if bid == nil || ask == nil {
		return nil
	}
	mid := (*bid + *ask) / 2
	if mid == 0 {
		return nil
	}
	v := (*ask - *bid) / mid * 10000
	return &v
}
