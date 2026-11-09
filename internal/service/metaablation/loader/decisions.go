package loader

import (
	"context"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisionsnapshot"
	"github.com/ousiassllc/pitha-trador/internal/repository/judgement"
	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/execution/enrich"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/labellayer"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// ReasonMarketDataUnavailable is the reason of a decision whose market_snapshots
// bar is not in the copy: its snapshot-derived conditions cannot be rebuilt, so
// the row is "unknown" (excluded from all four cells, counted).
const ReasonMarketDataUnavailable = "market_data_unavailable"

// Decisions are the stored Trader decisions of a period.
type Decisions struct {
	// Rows are the label layer's rows, one per Trader decision, in time order.
	Rows []labellayer.Row
	// All are the same decisions, enriched with the answer's fields as the
	// backtest reads them, for the replay layer (which keeps only the
	// independent ones).
	All []domain.JevDecision
	// OOSUsed are the IDs of the labeled decisions with complete features whose
	// label was complete by oosEnd: the rows an original OOS window could hold.
	OOSUsed map[int64]struct{}
}

// LoadDecisions reads the Trader decisions timestamped in [from, to) with their
// labels (label_version 2, net_return_bps; repo.ListTrainingRowsFrom, the
// training's own selection), the market_snapshots bar each was made on
// (decisionsnapshot, policy.InputFromSnapshot: the mapping live and the
// replay share) and the features rebuilt from the stored state and answer.
// oosEnd bounds OOSUsed. Scout decisions are not read.
func (s *Source) LoadDecisions(ctx context.Context, from, to, oosEnd time.Time) (Decisions, error) {
	labeled, err := repo.ListTrainingRowsFrom(ctx, s.db, from, to, repo.TrainingScope{})
	if err != nil {
		return Decisions{}, err
	}
	labels := make(map[int64]repo.TrainingRow, len(labeled))
	for _, tr := range labeled {
		labels[tr.JevDecisionID] = tr
	}
	bars := map[int64]policy.Input{}
	err = decisionsnapshot.New(s.db).EachInRange(ctx, from, to, func(p decisionsnapshot.Pair) error {
		bars[p.DecisionID] = policy.InputFromSnapshot(p.Snapshot)
		return nil
	})
	if err != nil {
		return Decisions{}, err
	}
	out := Decisions{OOSUsed: map[int64]struct{}{}}
	err = judgement.NewDecisionRepository(s.db).EachInRange(ctx, from, to, func(raw domain.JevDecision) error {
		if raw.DecisionType != domain.JevDecisionTypeTrader {
			return nil
		}
		d := enrich.Decision(raw)
		tr, isLabeled := labels[d.ID]
		row := labellayer.Row{ID: d.ID, DecidedAt: d.Timestamp, Cohort: Cohort(d), NetReturnBps: tr.NetReturnBps}
		if d.Direction != nil && (*d.Direction == domain.JevDirectionLong || *d.Direction == domain.JevDirectionShort) {
			row.Direction = *d.Direction
		}
		in := eligibility.Input{ID: d.ID, Labeled: isLabeled, Features: BuildFeatures(d)}
		bar, hasBar := bars[d.ID]
		if !hasBar {
			in.Unrecoverable = ReasonMarketDataUnavailable
		}
		bar.InstrumentID, bar.Symbol, bar.Timestamp, bar.Decision = d.InstrumentID, d.Symbol, d.Timestamp, &d
		in.Policy = bar
		row.Eligibility = in
		if isLabeled && len(in.Features.MissingNames()) == 0 && !tr.LabelEnd().After(oosEnd) {
			out.OOSUsed[d.ID] = struct{}{}
		}
		out.Rows = append(out.Rows, row)
		out.All = append(out.All, d)
		return nil
	})
	if err != nil {
		return Decisions{}, fmt.Errorf("loader: read decisions: %w", err)
	}
	return out, nil
}
