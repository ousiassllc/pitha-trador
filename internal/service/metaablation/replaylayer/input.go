package replaylayer

import (
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/backtest/simrisk"
	"github.com/ousiassllc/pitha-trador/internal/service/fillmodel"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// Instrument is one instrument's market data: the exact ascending-by-Timestamp
// snapshot series (with leading warm-up history before the window, see
// backtest.RunConfig.Snapshots).
type Instrument struct {
	InstrumentID int64
	Symbol       string
	// AssetClass is the instrument's asset_class (domain.AssetClass*; empty is
	// a stock).
	AssetClass string
	Snapshots  []domain.Snapshot
	// WarmupBars is how many leading Snapshots are history only; they must
	// precede the window.
	WarmupBars int
}

// Unavailable is a period the caller found to have missing market_snapshots.
// It is expanded to whole JST days.
type Unavailable struct {
	From, To time.Time
	Reason   string
}

// Input is everything the replay layer needs, in memory: it reads no database.
type Input struct {
	// From and To are the evaluation window [From, To), the same for all cells.
	From, To time.Time
	// Instruments are the market data of every replayed instrument.
	Instruments []Instrument
	// Decisions are the stored Jev decisions to replay, ONLY the independent
	// ones (the caller's contract; this package never decides independence).
	// They also feed the exit rules (Jev direction reversal), so they must be
	// the complete independent set of the window.
	Decisions []domain.JevDecision

	// Thresholds are the pre-registered guard values shared by all cells. Its
	// MinEntryQuality values are ignored: the Q axis sets them per cell. Its
	// MinProbability (the old confidence threshold) is never used.
	Thresholds policy.Thresholds
	// Risk is the simulated Risk Engine configuration, the same for all cells;
	// Risk.Limits.InitialCapital is the common initial capital.
	Risk simrisk.Config
	// Cost is the fill and fee assumption; Exit the FR-EXIT-1 exit rule;
	// Sessions the 立会時間 classification (nil: every bar is ザラ場).
	Cost     fillmodel.Costs
	Exit     backtest.ExitRule
	Sessions backtest.Sessions

	// Model is the stored model to re-score, parsed from its params
	// (metalabel.ParseModel or equivalent); never trained or calibrated here.
	Model *metalabel.Model
	// Meta is the pre-registered take/skip policy (minimum EV).
	Meta metalabel.DecideConfig

	// Unavailable lists the periods with missing market_snapshots.
	Unavailable []Unavailable
	// ReferenceDailyReset additionally runs the clearly labelled reference
	// experiment replaying every day independently.
	ReferenceDailyReset bool
}

// capital is the common initial capital.
func (in Input) capital() float64 { return in.Risk.Limits.InitialCapital }

func (in Input) validate() error {
	switch {
	case !in.From.Before(in.To):
		return fmt.Errorf("replaylayer: window [%s, %s) is empty", in.From, in.To)
	case len(in.Instruments) == 0:
		return errors.New("replaylayer: no instruments")
	case !(in.capital() > 0):
		return errors.New("replaylayer: Risk.Limits.InitialCapital must be positive (the common initial capital)")
	case in.Model == nil:
		return errors.New("replaylayer: no model to re-score: the M-on cells cannot run")
	}
	if err := in.Meta.Validate(); err != nil {
		return fmt.Errorf("replaylayer: %w", err)
	}
	seen := map[int64]bool{}
	for _, inst := range in.Instruments {
		if seen[inst.InstrumentID] {
			return fmt.Errorf("replaylayer: instrument %d given twice", inst.InstrumentID)
		}
		seen[inst.InstrumentID] = true
		if v := backtest.VerifyNoLookahead(inst.Snapshots, inst.WarmupBars); len(v) > 0 {
			return fmt.Errorf("replaylayer: %s: %d bar(s) fail the look-ahead check (FR-BT-3), e.g. %s", inst.Symbol, len(v), v[0])
		}
		first := firstIndexFrom(inst.Snapshots, in.From)
		if inst.WarmupBars < 0 || inst.WarmupBars > first {
			return fmt.Errorf("replaylayer: %s: WarmupBars %d must be within the history before the window (%d bars)", inst.Symbol, inst.WarmupBars, first)
		}
	}
	for _, u := range in.Unavailable {
		if !u.From.Before(u.To) {
			return fmt.Errorf("replaylayer: unavailable period [%s, %s) is empty", u.From, u.To)
		}
		if u.Reason == "" {
			return errors.New("replaylayer: an unavailable period needs a reason")
		}
	}
	return nil
}
