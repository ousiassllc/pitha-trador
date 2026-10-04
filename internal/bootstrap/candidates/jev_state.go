package candidates

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/enrich"
)

// attachJevState fills each candidate's JevDirection/JevConfidence/
// EntryQuality from the instrument's latest Jev Trader decision and its
// CurrentPosition from the open position (functional.md §5.1; issue #492),
// so the Scanner Dashboard/API/WebSocket show them rather than the nil
// "pending"/"flat" screener.Run leaves.
//
// It costs two queries per refresh cycle regardless of the candidate count
// (one batched latest-decision read, one open-position read), not one per
// candidate. A candidate with no Trader decision keeps nil Jev fields, and
// one with no open position keeps a nil CurrentPosition. EntryQuality is
// stored only inside the decision's response_json, so it is read through
// enrich.Decision exactly as the Symbol Detail API and Execution do.
// CurrentPosition is signed like GET /api/v1/symbols/{symbol}'s
// current_position: positive for LONG, negative for SHORT.
func (r *Refresher) attachJevState(ctx context.Context, cs []domain.Candidate) error {
	if len(cs) == 0 {
		return nil
	}
	ids := make([]int64, len(cs))
	for i, c := range cs {
		ids[i] = c.InstrumentID
	}

	decisions, err := r.Decisions.LatestTraderByInstruments(ctx, ids)
	if err != nil {
		return fmt.Errorf("candidates: latest trader decisions: %w", err)
	}
	positions, err := r.Positions.ListOpen(ctx)
	if err != nil {
		return fmt.Errorf("candidates: list open positions: %w", err)
	}
	held := make(map[int64]float64, len(positions))
	for _, p := range positions {
		size := float64(p.Quantity)
		if p.Side == domain.PositionSideShort {
			size = -size
		}
		held[p.InstrumentID] = size
	}

	for i := range cs {
		if d, ok := decisions[cs[i].InstrumentID]; ok {
			d = enrich.Decision(d)
			cs[i].JevDirection = d.Direction
			cs[i].JevConfidence = d.Confidence
			cs[i].EntryQuality = d.EntryQuality
		}
		if size, ok := held[cs[i].InstrumentID]; ok {
			cs[i].CurrentPosition = &size
		}
	}
	return nil
}
