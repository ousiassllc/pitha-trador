package dataexport

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/calibration"
	"github.com/ousiassllc/pitha-trador/internal/repository/decisionsnapshot"
	"github.com/ousiassllc/pitha-trador/internal/repository/snapshotcols"
)

// DecisionSource streams jev_decisions by decision time
// (judgement.DecisionRepository).
type DecisionSource interface {
	EachInRange(ctx context.Context, from, to time.Time, fn func(domain.JevDecision) error) error
}

// DecisionFeatureSource streams, for the decisions of a period, the
// market_snapshots bar each was made on (decisionsnapshot.Repository).
type DecisionFeatureSource interface {
	EachInRange(ctx context.Context, from, to time.Time, fn func(decisionsnapshot.Pair) error) error
}

// DecisionSignalSource streams the trade_signals derived from the decisions of
// a period (trading.SignalRepository).
type DecisionSignalSource interface {
	EachByDecisionRange(ctx context.Context, from, to time.Time, fn func(domain.TradeSignal) error) error
}

// LabelSource streams the after-the-fact labels of the decisions of a period
// (calibration.CalibrationRepository).
type LabelSource interface {
	EachOutcomeByDecisionRange(ctx context.Context, from, to time.Time, fn func(domain.CalibrationOutcome) error) error
	EachLabelSkipByDecisionRange(ctx context.Context, from, to time.Time, fn func(calibration.LabelSkip) error) error
}

// Decisions is the decision-log Collector (KindDecisions). Every table
// carries decision_id (= jev_decisions.id = trade_signals.jev_decision_id =
// calibration_outcomes.jev_decision_id), so the candidate, its features, the
// Jev request/response with latency, the skip/reject reasons and the labels
// join on that one key:
//
//   - decisions (primary): the Jev call - state_json is the request sent,
//     response_json the answer, with question_version, latency_ms, model and
//     the order-flow / regime context stored with it
//   - features: the market_snapshots bar (features) the decision was made on
//   - signals: the Policy Engine's outcome, incl. risk_passed / reject_reason
//   - outcomes: calibration_outcomes labels
//   - label_skips: calibration_label_skips (pairs that will never be labeled)
type Decisions struct {
	Decisions DecisionSource
	Features  DecisionFeatureSource
	Signals   DecisionSignalSource
	Labels    LabelSource
}

// Kind implements Collector.
func (Decisions) Kind() Kind { return KindDecisions }

// Tables implements Collector.
func (d Decisions) Tables() []Table {
	return []Table{d.decisionsTable(), d.featuresTable(), d.signalsTable(), d.outcomesTable(), d.labelSkipsTable()}
}

func (d Decisions) decisionsTable() Table {
	columns := cols("decision_id", "instrument_id", "symbol", "timestamp", "decision_type", "state_hash",
		"request_state_json", "question_version", "response_json", "direction", "confidence", "latency_ms", "model_id", "request_cost",
		"ofi_short", "ofi_mid", "ofi_long", "ofi_window_short_seconds", "ofi_window_mid_seconds", "ofi_window_long_seconds",
		"microprice_dev_bps", "data_age_ms", "market_regime", "ab_arm", "regime_volatility_bps", "regime_autocorr", "vwap_z", "created_at")
	// The stored Jev request (the state sent) and response are JSON documents.
	columns[6].JSON, columns[8].JSON = true, true
	return Table{
		Name:    "decisions",
		Columns: columns,
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return d.Decisions.EachInRange(ctx, p.From, p.To, func(j domain.JevDecision) error {
				return emit(Row{j.ID, j.InstrumentID, j.Symbol, j.Timestamp, j.DecisionType, j.StateHash,
					j.StateJSON, j.QuestionVersion, j.ResponseJSON, val(j.Direction), val(j.Confidence), j.LatencyMs, j.ModelID, val(j.RequestCost),
					val(j.OrderFlow.OFIShort), val(j.OrderFlow.OFIMid), val(j.OrderFlow.OFILong),
					val(j.OrderFlow.OFIWindowShortSeconds), val(j.OrderFlow.OFIWindowMidSeconds), val(j.OrderFlow.OFIWindowLongSeconds),
					val(j.OrderFlow.MicropriceDevBps), val(j.DataAgeMS),
					emptyAsNil(j.MarketRegime.Regime), emptyAsNil(j.MarketRegime.ABArm),
					val(j.MarketRegime.VolatilityBps), val(j.MarketRegime.Autocorr), val(j.MarketRegime.VWAPZ), j.CreatedAt})
			})
		},
	}
}

// featureLead are the market_snapshots columns that precede the Feature
// columns; the Feature columns come from snapshotcols.Columns, so a feature
// added to the snapshot is exported without touching this file.
var featureLead = []string{"decision_id", "snapshot_id", "snapshot_timestamp", "price", "bid", "ask", "spread_bps", "volume", "turnover"}

func (d Decisions) featuresTable() Table {
	names := append([]string{}, featureLead...)
	for _, c := range snapshotcols.Columns {
		names = append(names, c.Name)
	}
	return Table{
		Name:    "features",
		Columns: cols(names...),
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return d.Features.EachInRange(ctx, p.From, p.To, func(ds decisionsnapshot.Pair) error {
				s := ds.Snapshot
				row := make(Row, 0, len(names))
				row = append(row, ds.DecisionID, s.ID, s.Timestamp, s.Price, val(s.Bid), val(s.Ask), val(s.SpreadBps), s.Volume, s.Turnover)
				for _, c := range snapshotcols.Columns {
					row = append(row, c.Arg(&s))
				}
				return emit(row)
			})
		},
	}
}

func (d Decisions) signalsTable() Table {
	return Table{
		Name: "signals",
		Columns: cols("signal_id", "decision_id", "instrument_id", "symbol", "timestamp", "direction", "score",
			"entry_price_reference", "policy_version", "risk_passed", "reject_reason"),
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return d.Signals.EachByDecisionRange(ctx, p.From, p.To, func(s domain.TradeSignal) error {
				return emit(signalRow(s))
			})
		},
	}
}

func (d Decisions) outcomesTable() Table {
	return Table{
		Name: "outcomes",
		Columns: cols("outcome_id", "decision_id", "horizon_minutes", "future_return", "max_adverse_excursion",
			"max_favorable_excursion", "was_direction_correct", "barrier_hit", "net_return_bps", "entry_ref_price",
			"label_version", "created_at"),
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return d.Labels.EachOutcomeByDecisionRange(ctx, p.From, p.To, func(o domain.CalibrationOutcome) error {
				return emit(Row{o.ID, o.JevDecisionID, o.HorizonMinutes, o.FutureReturn, o.MaxAdverseExcursion,
					o.MaxFavorableExcursion, val(o.WasDirectionCorrect), emptyAsNil(o.BarrierHit), val(o.NetReturnBps),
					val(o.EntryRefPrice), o.LabelVersion, o.CreatedAt})
			})
		},
	}
}

func (d Decisions) labelSkipsTable() Table {
	return Table{
		Name:    "label_skips",
		Columns: cols("decision_id", "horizon_minutes", "reason", "created_at"),
		Read: func(ctx context.Context, p Period, emit func(Row) error) error {
			return d.Labels.EachLabelSkipByDecisionRange(ctx, p.From, p.To, func(s calibration.LabelSkip) error {
				return emit(Row{s.JevDecisionID, s.HorizonMinutes, s.Reason, s.CreatedAt})
			})
		},
	}
}

// emptyAsNil maps "" (not set / not assessed) to NULL, as the tables store it.
func emptyAsNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}
