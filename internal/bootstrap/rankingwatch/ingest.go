package rankingwatch

import (
	"context"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// IngestSet is the instruments one watch cycle enqueues market-data jobs for:
// the watched symbols that are in the universe (byCode, by symbol code), plus
// the index rows the market context needs (market_return_*, sector_return_5m;
// FR-FE-4), which the full scan ingests along with every stock but a watch
// list does not contain (issue #670; without them the Risk Engine's
// market_adverse_to_direction gate would find no market return): every active
// market_index row and the sector_index rows of the watched stocks' sectors.
// A failed index listing is reported to onIndexError and skipped.
func IngestSet(ctx context.Context, universe Universe, watch []string, byCode map[string]domain.Instrument, onIndexError func(error)) []domain.Instrument {
	instruments := make([]domain.Instrument, 0, len(watch))
	sectors := make(map[string]struct{})
	for _, sym := range watch {
		if inst, ok := byCode[sym]; ok {
			instruments = append(instruments, inst)
			if inst.Sector != nil {
				sectors[*inst.Sector] = struct{}{}
			}
		}
	}
	for _, kind := range []string{domain.InstrumentKindMarketIndex, domain.InstrumentKindSectorIndex} {
		rows, err := universe.ListActiveByKind(ctx, kind)
		if err != nil {
			onIndexError(err)
			continue
		}
		for _, inst := range rows {
			if kind == domain.InstrumentKindSectorIndex {
				if inst.Sector == nil {
					continue
				}
				if _, ok := sectors[*inst.Sector]; !ok {
					continue
				}
			}
			instruments = append(instruments, inst)
		}
	}
	return instruments
}
