package ledgerdiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
)

// decRow is one evaluable trader LONG/SHORT decision on every label arm: the net bps (after the
// label's cost) of a LONG and a SHORT of it, the baselines, and its identity.
type decRow struct {
	ID               int64
	At               time.Time
	Day              string
	Model, Version   string
	Dir              string
	Long, Short      float64
	Random, Momentum float64
	Contrarian       float64
	// CostUSD is jev_decisions.request_cost; HasCost is false when it was not recorded (NULL).
	CostUSD float64
	HasCost bool
}

// own is the net bps of the decision's own direction.
func (d decRow) own() float64 { return d.dirNet(d.Dir) }

// dirNet is the net bps of answering dir on this row: NONE is no trade (0 bps).
func (d decRow) dirNet(dir string) float64 {
	switch dir {
	case domain.JevDirectionLong:
		return d.Long
	case domain.JevDirectionShort:
		return d.Short
	}
	return 0
}

// baselineNet is the net bps of a baseline arm (jev_evaluation's definitions).
func (d decRow) baselineNet(arm string) float64 {
	switch arm {
	case evaldiag.ArmRandom:
		return d.Random
	case evaldiag.ArmMomentum:
		return d.Momentum
	case evaldiag.ArmContrarian:
		return d.Contrarian
	case evaldiag.ArmLongLeg:
		return d.Long
	case evaldiag.ArmShortLeg:
		return d.Short
	}
	return 0
}

// dataset is the evaluable decisions of the period (every cohort, time order) with what was left out.
type dataset struct {
	liveVersion string
	cutoff      evaldiag.Cutoff
	rows        []decRow
	excl        Exclusions
}

// settingsReader is a read-only runtime_settings view limited to the metalabel.* keys the LLM
// cutoff needs: runtime_settings can hold secret paths, so any other key is refused before SQL runs.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !slices.Contains(metalabelsettings.Keys(), key) {
		return "", false, fmt.Errorf("ledgerdiag: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("ledgerdiag: read runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// readCutoff is the LLM training cutoff (the way evaldiag reads it): start is the JST start of the cutoff
// date, the first instant a decision counts; the zero time while the cutoff is the placeholder.
func readCutoff(ctx context.Context, q query.Q) (evaldiag.Cutoff, time.Time, error) {
	s, err := metalabelsettings.Load(ctx, settingsReader{q})
	if err != nil {
		return evaldiag.Cutoff{}, time.Time{}, fmt.Errorf("ledgerdiag: %w", err)
	}
	c := evaldiag.Cutoff{Date: s.LLMCutoff, Status: s.CutoffStatus(), Model: s.LLMCutoffModel}
	if c.Status == metalabelsettings.CutoffPlaceholder {
		return c, time.Time{}, nil
	}
	day, err := time.ParseInLocation(metalabelsettings.LLMCutoffLayout, s.LLMCutoff, query.JST)
	if err != nil {
		return evaldiag.Cutoff{}, time.Time{}, fmt.Errorf("ledgerdiag: %s %q is not a date: %w", metalabelsettings.KeyLLMCutoff, s.LLMCutoff, err)
	}
	c.Applied = true
	return c, day, nil
}

// rawDecision is a decision of the period as read, before the label and cutoff exclusions.
type rawDecision struct {
	id                int64
	at                time.Time
	dir               string
	model, version    string
	cost              sql.NullFloat64
	hasBar            bool
	return5m          sql.NullFloat64
	longNet, shortNet sql.NullFloat64
}

// listDecisions reads every trader LONG/SHORT decision of [from, to), oldest first, with the
// research label of the horizon joined on (instrument, snapshot time = the decision's timestamp)
// when horizon is not 10m. More than evaldiag.MaxDecisions is query.ErrLimitExceeded.
func listDecisions(ctx context.Context, q query.Q, from, to, horizon string) ([]rawDecision, error) {
	labelCols, labelJoin, args := "NULL, NULL", "", []any{}
	if horizon != Horizon10m {
		labelCols = "l.long_net_bps, l.short_net_bps"
		labelJoin = ` LEFT JOIN research_horizon_labels l ON l.instrument_id = d.instrument_id AND l.snapshot_at = d.timestamp AND l.horizon = ?`
		args = append(args, horizon)
	}
	args = append(args, from, to, evaldiag.MaxDecisions+1)
	rows, err := q.QueryContext(ctx, `
SELECT d.id, d.timestamp, d.direction, d.model_id, d.question_version, d.request_cost,
	m.id IS NOT NULL, m.return_5m, `+labelCols+`
FROM jev_decisions d
LEFT JOIN market_snapshots m ON m.instrument_id = d.instrument_id AND m.timestamp = d.timestamp`+labelJoin+`
WHERE d.decision_type = 'trader' AND d.direction IN ('LONG', 'SHORT') AND d.timestamp >= ? AND d.timestamp < ?
ORDER BY d.timestamp, d.id LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("query: list ledger decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []rawDecision
	for rows.Next() {
		var (
			d  rawDecision
			at string
		)
		if err := rows.Scan(&d.id, &at, &d.dir, &d.model, &d.version, &d.cost, &d.hasBar, &d.return5m, &d.longNet, &d.shortNet); err != nil {
			return nil, fmt.Errorf("query: scan ledger decision: %w", err)
		}
		if len(out) == evaldiag.MaxDecisions {
			return nil, query.ErrLimitExceeded
		}
		if d.at, err = sqlutil.ParseTime(at); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: list ledger decisions: %w", err)
	}
	return out, nil
}

// load reads the live version, the cutoff and the decisions of the period and labels each on every
// arm at the request's horizon. A decision that cannot be labeled on both sides (or is before the
// cutoff) is left out for every candidate and counted.
func load(ctx context.Context, q query.Q, r query.Range, req Request) (*dataset, error) {
	live, err := jev.ResolveLiveVersion(ctx, shadowquery.SettingsStore(q))
	if err != nil {
		return nil, fmt.Errorf("ledgerdiag: %w", err)
	}
	cutoff, cutoffStart, err := readCutoff(ctx, q)
	if err != nil {
		return nil, err
	}
	from, to := r.Bounds()
	horizon := req.horizon()
	raws, err := listDecisions(ctx, q, from, to, horizon)
	if err != nil {
		return nil, err
	}
	ds := &dataset{liveVersion: live, cutoff: cutoff, excl: Exclusions{Decisions: len(raws)}}

	var labeled map[int64]evaldiag.Row // the 10m labels are the jev_evaluation's own
	if horizon == Horizon10m {
		ten := 10
		evalRows, _, err := evaldiag.Rows(ctx, q, r, evaldiag.Request{HorizonMinutes: &ten})
		if err != nil {
			return nil, err
		}
		labeled = make(map[int64]evaldiag.Row, len(evalRows))
		for _, e := range evalRows {
			labeled[e.DecisionID] = e
		}
	}
	for _, d := range raws {
		if cutoff.Applied && d.at.Before(cutoffStart) {
			ds.excl.BeforeCutoff++
			continue
		}
		row := decRow{
			ID: d.id, At: d.at, Day: d.at.In(query.JST).Format(time.DateOnly), Model: d.model, Version: d.version, Dir: d.dir,
			CostUSD: d.cost.Float64, HasCost: d.cost.Valid,
		}
		if horizon == Horizon10m {
			e, ok := labeled[d.id]
			if !ok {
				ds.excl.NoLabel++
				continue
			}
			row.Long, row.Short, row.Random, row.Momentum, row.Contrarian = e.Long, e.Short, e.Random, e.Momentum, e.Contrarian
		} else {
			switch {
			case !d.longNet.Valid || !d.shortNet.Valid:
				ds.excl.NoLabel++
				continue
			case !d.hasBar:
				ds.excl.NoBarForMomentum++
				continue
			}
			row.Long, row.Short = d.longNet.Float64, d.shortNet.Float64
			row.Random = (row.Long + row.Short) / 2
			switch {
			case d.return5m.Valid && d.return5m.Float64 > 0:
				row.Momentum, row.Contrarian = row.Long, row.Short
			case d.return5m.Valid && d.return5m.Float64 < 0:
				row.Momentum, row.Contrarian = row.Short, row.Long
			}
		}
		ds.rows = append(ds.rows, row)
	}
	ds.excl.Evaluated = len(ds.rows)
	return ds, nil
}
