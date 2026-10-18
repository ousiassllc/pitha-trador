package actuals

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
)

// Reader reads the evaluation inputs from the SQLite database.
type Reader struct {
	db *sql.DB
}

// NewReader returns a Reader backed by db.
func NewReader(db *sql.DB) *Reader {
	return &Reader{db: db}
}

// closedTradesSQL reads every closed position with its entry/exit orders
// (fees, slippage_bps) and, through LEFT JOINs, the A/B arm and the newest
// meta-labeling prediction of the decision behind the entry: a manual entry
// (no signal/decision) is still a closed trade. A decision can carry one
// prediction per model; the newest one (highest id) is the one that was
// current when the entry was decided. Rows with a NULL realized_pnl cannot
// have been closed through PositionRepository.Close and are skipped.
const closedTradesSQL = `
SELECT p.id, p.side, p.quantity, p.entry_price, x.filled_price, p.realized_pnl,
	p.opened_at, p.closed_at, COALESCE(p.exit_reason, ''),
	eo.fees, COALESCE(x.fees, 0), eo.slippage_bps, x.slippage_bps,
	COALESCE(d.ab_arm, ''), COALESCE(m.version, ''), mp.expected_value_bps
FROM positions p
JOIN paper_orders eo ON eo.id = p.entry_order_id
LEFT JOIN paper_orders x ON x.id = p.exit_order_id
LEFT JOIN trade_signals s ON s.id = eo.trade_signal_id
LEFT JOIN jev_decisions d ON d.id = s.jev_decision_id
LEFT JOIN metalabel_predictions mp
	ON mp.id = (SELECT MAX(id) FROM metalabel_predictions WHERE jev_decision_id = d.id)
LEFT JOIN metalabel_models m ON m.id = mp.model_id
WHERE p.closed_at IS NOT NULL AND p.realized_pnl IS NOT NULL
ORDER BY p.closed_at ASC, p.id ASC`

// ClosedTrades returns every closed Paper position, oldest close first.
func (r *Reader) ClosedTrades(ctx context.Context) ([]Trade, error) {
	var out []Trade
	collect := func(t Trade) error { out = append(out, t); return nil }
	if err := sqlutil.Each(ctx, r.db, scanTrade, collect, closedTradesSQL); err != nil {
		return nil, fmt.Errorf("actuals: read closed trades: %w", err)
	}
	return out, nil
}

func scanTrade(row sqlutil.RowScanner) (Trade, error) {
	var (
		t                  Trade
		exitPrice          sql.NullFloat64
		openedAt, closedAt string
	)
	err := row.Scan(&t.PositionID, &t.Side, &t.Quantity, &t.EntryPrice, &exitPrice, &t.RealizedPnL,
		&openedAt, &closedAt, &t.ExitReason,
		&t.EntryFee, &t.ExitFee, sqlutil.NullFloat(&t.EntrySlippageBps), sqlutil.NullFloat(&t.ExitSlippageBps),
		&t.ABArm, &t.MetaModelVersion, sqlutil.NullFloat(&t.MetaExpectedBps))
	if err != nil {
		return Trade{}, fmt.Errorf("actuals: scan closed trade: %w", err)
	}
	if exitPrice.Valid {
		t.ExitPrice = &exitPrice.Float64
	}
	if t.OpenedAt, err = sqlutil.ParseTime(openedAt); err != nil {
		return Trade{}, fmt.Errorf("actuals: parse opened_at of position %d: %w", t.PositionID, err)
	}
	if t.ClosedAt, err = sqlutil.ParseTime(closedAt); err != nil {
		return Trade{}, fmt.Errorf("actuals: parse closed_at of position %d: %w", t.PositionID, err)
	}
	return t, nil
}

// jevSamplesSQL reads the graded directional trader decisions. A tie (a
// return of exactly zero: the close-only label's future_return, or the
// triple-barrier label's net_return_bps) is not a hit or a miss and is left
// out. The label versions are returned side by side, never mixed.
const jevSamplesSQL = `
SELECT d.timestamp, d.direction, d.confidence, o.label_version, o.horizon_minutes, o.was_direction_correct
FROM calibration_outcomes o
JOIN jev_decisions d ON d.id = o.jev_decision_id
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.confidence IS NOT NULL
	AND o.was_direction_correct IS NOT NULL
	AND NOT ((o.label_version = 1 AND o.future_return = 0) OR (o.label_version <> 1 AND COALESCE(o.net_return_bps, 1) = 0))
ORDER BY d.timestamp ASC, o.id ASC`

// JevSamples returns every graded directional trader decision, oldest first.
func (r *Reader) JevSamples(ctx context.Context) ([]JevSample, error) {
	var out []JevSample
	collect := func(s JevSample) error { out = append(out, s); return nil }
	if err := sqlutil.Each(ctx, r.db, scanJevSample, collect, jevSamplesSQL); err != nil {
		return nil, fmt.Errorf("actuals: read jev samples: %w", err)
	}
	return out, nil
}

func scanJevSample(row sqlutil.RowScanner) (JevSample, error) {
	var (
		s         JevSample
		timestamp string
	)
	if err := row.Scan(&timestamp, &s.Direction, &s.Confidence, &s.LabelVersion, &s.HorizonMinutes, &s.Correct); err != nil {
		return JevSample{}, fmt.Errorf("actuals: scan jev sample: %w", err)
	}
	var err error
	if s.DecidedAt, err = sqlutil.ParseTime(timestamp); err != nil {
		return JevSample{}, fmt.Errorf("actuals: parse decision timestamp: %w", err)
	}
	return s, nil
}

const metaModelsSQL = `
SELECT version, algorithm, status, label_version, trained_at, oos_start, oos_end, oos_samples,
	oos_auc, oos_expected_value_bps
FROM metalabel_models
ORDER BY trained_at ASC, id ASC`

// MetaModels returns every trained meta-labeling model (active or rejected),
// oldest first.
func (r *Reader) MetaModels(ctx context.Context) ([]MetaModel, error) {
	var out []MetaModel
	collect := func(m MetaModel) error { out = append(out, m); return nil }
	if err := sqlutil.Each(ctx, r.db, scanMetaModel, collect, metaModelsSQL); err != nil {
		return nil, fmt.Errorf("actuals: read meta models: %w", err)
	}
	return out, nil
}

func scanMetaModel(row sqlutil.RowScanner) (MetaModel, error) {
	var (
		m                           MetaModel
		trainedAt, oosStart, oosEnd string
	)
	err := row.Scan(&m.Version, &m.Algorithm, &m.Status, &m.LabelVersion, &trainedAt, &oosStart, &oosEnd, &m.OOSSamples,
		sqlutil.NullFloat(&m.OOSAUC), sqlutil.NullFloat(&m.OOSExpectedBps))
	if err != nil {
		return MetaModel{}, fmt.Errorf("actuals: scan meta model: %w", err)
	}
	for _, f := range []struct {
		raw string
		dst *time.Time
	}{{trainedAt, &m.TrainedAt}, {oosStart, &m.OOSStart}, {oosEnd, &m.OOSEnd}} {
		if *f.dst, err = sqlutil.ParseTime(f.raw); err != nil {
			return MetaModel{}, fmt.Errorf("actuals: parse metalabel_models %q time: %w", m.Version, err)
		}
	}
	return m, nil
}
