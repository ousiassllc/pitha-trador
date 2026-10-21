package shadowquery

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// here as one scalar (json_extract) - the raw response_json is never returned.
const pairsCTE = `
WITH pairs AS (
	SELECT s.prompt_version AS version, s.entry_quality AS s_eq, s.direction AS s_dir,
		s.request_cost AS cost, s.latency_ms AS latency,
		d.id AS did, json_extract(d.response_json, '$.entry_quality') AS l_eq, d.direction AS l_dir, d.question_version AS l_version
	FROM jev_shadow_decisions s
	JOIN jev_decisions d ON d.id = s.jev_decision_id
	WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ?
		AND (? = '' OR s.prompt_version = ?)
)`

// Compare compares the live Trader decisions of r with the shadow's
// decisions for the same inputs, per candidate prompt version (version ""
// means every version present): the entry_quality distribution of both sides
// (poor to exceptional, counts and shares), the direction agreement rate, the
// shadow's cost, and the outcomes by entry_quality from calibration_outcomes
// (the same join as JevCalibration: jev_decision_id).
//
// Outcome method. calibration_outcomes is labeled for the LIVE direction: its
// was_direction_correct, barrier_hit and net_return_bps describe the live
// direction, and its future_return is the price return (percent) from the
// live direction's entry reference to the end of the label. For the shadow side
// the result is recomputed from future_return and the shadow's own direction:
// directional return = future_return for LONG, -future_return for SHORT; a
// NONE decision is not a trade (counted in Samples, not Directional); the
// shadow "hits" when the directional return is above 0. The live side uses the
// same formula so the two are comparable (OutcomeStats.HitRate,
// AvgDirectionalReturnPct); the label's own net-of-cost fields are reported
// separately (Recorded*) and only where the side's direction equals the live
// one. The recomputation is gross (no cost) and approximate: it reuses the
// live label's entry price and end bar, so it is evidence for comparing
// distributions, not a simulated P&L. Real P&L is judged after a promotion by
// the #871 experiments.
func Compare(ctx context.Context, q query.Q, r query.Range, version string) (ShadowComparison, error) {
	from, to := r.Bounds()
	args := []any{from, to, version, version}
	out := ShadowComparison{Versions: []ShadowVersionComparison{}}
	index := map[string]int{}
	slot := func(v string) (*ShadowVersionComparison, error) {
		if i, ok := index[v]; ok {
			return &out.Versions[i], nil
		}
		if len(out.Versions) == MaxShadowVersions {
			return nil, query.ErrLimitExceeded
		}
		index[v] = len(out.Versions)
		out.Versions = append(out.Versions, ShadowVersionComparison{
			PromptVersion: v, LiveVersions: []VersionShare{}, Outcomes: []HorizonOutcomes{},
			EntryQuality: zeroQualityCounts(),
		})
		return &out.Versions[len(out.Versions)-1], nil
	}

	// Pairs, direction agreement and cost per candidate version.
	rows, err := q.QueryContext(ctx, pairsCTE+`
SELECT version, COUNT(*), SUM(s_dir = l_dir), COALESCE(SUM(cost), 0), AVG(latency)
FROM pairs GROUP BY version ORDER BY version`, args...)
	if err != nil {
		return ShadowComparison{}, fmt.Errorf("query: jev shadow pairs: %w", err)
	}
	for rows.Next() {
		var (
			v          string
			pairs, agr int
			cost       float64
			latency    sql.NullFloat64
		)
		if err := rows.Scan(&v, &pairs, &agr, &cost, &latency); err != nil {
			_ = rows.Close()
			return ShadowComparison{}, fmt.Errorf("query: scan jev shadow pairs: %w", err)
		}
		c, err := slot(v)
		if err != nil {
			_ = rows.Close()
			return ShadowComparison{}, err
		}
		c.Pairs = pairs
		c.DirectionAgreement = DirectionAgreement{Agree: agr, Rate: ratio(agr, pairs)}
		c.Cost = ShadowCost{Calls: pairs, RequestCostUSD: cost, AvgLatencyMs: floatPtr(latency)}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return ShadowComparison{}, fmt.Errorf("query: jev shadow pairs: %w", err)
	}
	_ = rows.Close()
	if len(out.Versions) == 0 {
		return out, nil
	}

	if err := fillLiveVersions(ctx, q, &out, index, args); err != nil {
		return ShadowComparison{}, err
	}
	if err := fillDistribution(ctx, q, &out, index, args); err != nil {
		return ShadowComparison{}, err
	}
	if err := fillOutcomes(ctx, q, &out, index, args); err != nil {
		return ShadowComparison{}, err
	}
	return out, nil
}

func zeroQualityCounts() []QualityCount {
	out := make([]QualityCount, len(entryQualityOrder))
	for i, eq := range entryQualityOrder {
		out[i].EntryQuality = eq
	}
	return out
}

func qualityIndex(eq string) int {
	for i, name := range entryQualityOrder {
		if name == eq {
			return i
		}
	}
	return -1
}

func fillLiveVersions(ctx context.Context, q query.Q, out *ShadowComparison, index map[string]int, args []any) error {
	rows, err := q.QueryContext(ctx, pairsCTE+`
SELECT version, l_version, COUNT(*) FROM pairs GROUP BY version, l_version ORDER BY version, l_version`, args...)
	if err != nil {
		return fmt.Errorf("query: jev shadow live versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var v, live string
		var n int
		if err := rows.Scan(&v, &live, &n); err != nil {
			return fmt.Errorf("query: scan jev shadow live versions: %w", err)
		}
		c := &out.Versions[index[v]]
		c.LiveVersions = append(c.LiveVersions, VersionShare{Version: live, Pairs: n})
	}
	return rows.Err()
}

func fillDistribution(ctx context.Context, q query.Q, out *ShadowComparison, index map[string]int, args []any) error {
	rows, err := q.QueryContext(ctx, pairsCTE+`
SELECT 'live', version, l_eq, COUNT(*) FROM pairs GROUP BY version, l_eq
UNION ALL
SELECT 'shadow', version, s_eq, COUNT(*) FROM pairs GROUP BY version, s_eq`, args...)
	if err != nil {
		return fmt.Errorf("query: jev shadow distribution: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var side, v string
		var eq sql.NullString
		var n int
		if err := rows.Scan(&side, &v, &eq, &n); err != nil {
			return fmt.Errorf("query: scan jev shadow distribution: %w", err)
		}
		c := &out.Versions[index[v]]
		i := qualityIndex(eq.String)
		if !eq.Valid || i < 0 {
			continue // a live row without a valid entry_quality has no level to count under
		}
		count := Count{Count: n, Share: ratio(n, c.Pairs)}
		if side == "live" {
			c.EntryQuality[i].Live = count
		} else {
			c.EntryQuality[i].Shadow = count
		}
	}
	return rows.Err()
}

// outcomeSide is the SQL of one side's outcome rows: side, version, level,
// horizon, label version, the side's direction, the live direction and the
// outcome columns.
func outcomeSide(side, eqCol, dirCol string) string {
	return `SELECT '` + side + `' AS side, p.version AS version, p.` + eqCol + ` AS eq, co.horizon_minutes AS horizon, co.label_version AS lv,
	p.` + dirCol + ` AS dir, p.l_dir AS live_dir, co.future_return AS fr, co.was_direction_correct AS wdc, co.barrier_hit AS bh, co.net_return_bps AS nrb
FROM pairs p JOIN calibration_outcomes co ON co.jev_decision_id = p.did`
}

func fillOutcomes(ctx context.Context, q query.Q, out *ShadowComparison, index map[string]int, args []any) error {
	rows, err := q.QueryContext(ctx, pairsCTE+`
SELECT side, version, horizon, lv, eq, COUNT(*),
	COALESCE(SUM(dir <> 'NONE'), 0),
	COALESCE(SUM(dir <> 'NONE' AND (CASE dir WHEN 'LONG' THEN 1.0 ELSE -1.0 END) * fr > 0), 0),
	AVG(CASE WHEN dir <> 'NONE' THEN (CASE dir WHEN 'LONG' THEN 1.0 ELSE -1.0 END) * fr END),
	COALESCE(SUM(dir = live_dir AND wdc IS NOT NULL), 0),
	COALESCE(SUM(dir = live_dir AND wdc = 1), 0),
	COALESCE(SUM(dir = live_dir AND bh = 'tp'), 0), COALESCE(SUM(dir = live_dir AND bh = 'sl'), 0),
	COALESCE(SUM(dir = live_dir AND bh = 'timeout'), 0),
	AVG(CASE WHEN dir = live_dir THEN nrb END)
FROM (`+outcomeSide("live", "l_eq", "l_dir")+`
	UNION ALL `+outcomeSide("shadow", "s_eq", "s_dir")+`)
GROUP BY version, horizon, lv, eq, side
ORDER BY version, horizon, lv`, args...)
	if err != nil {
		return fmt.Errorf("query: jev shadow outcomes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			side, v        string
			horizon, lv    int
			eq             sql.NullString
			s              OutcomeStats
			hits           int
			avgRet, avgNet sql.NullFloat64
			recHits        int
		)
		if err := rows.Scan(&side, &v, &horizon, &lv, &eq, &s.Samples, &s.Directional, &hits, &avgRet,
			&s.RecordedSamples, &recHits, &s.RecordedBarrierCounts.TP, &s.RecordedBarrierCounts.SL,
			&s.RecordedBarrierCounts.Timeout, &avgNet); err != nil {
			return fmt.Errorf("query: scan jev shadow outcomes: %w", err)
		}
		i := qualityIndex(eq.String)
		if !eq.Valid || i < 0 {
			continue
		}
		s.HitRate = ratio(hits, s.Directional)
		s.AvgDirectionalReturnPct = floatPtr(avgRet)
		s.RecordedHitRate = ratio(recHits, s.RecordedSamples)
		if lv == domain.CalibrationLabelVersionTripleBarrier {
			s.RecordedAvgNetReturnBps = floatPtr(avgNet)
		} else {
			s.RecordedBarrierCounts = query.BarrierCounts{}
		}
		c := &out.Versions[index[v]]
		h := horizonSlot(c, horizon, lv)
		if side == "live" {
			h.ByEntryQuality[i].Live = s
		} else {
			h.ByEntryQuality[i].Shadow = s
		}
	}
	return rows.Err()
}

func horizonSlot(c *ShadowVersionComparison, horizon, lv int) *HorizonOutcomes {
	for i := range c.Outcomes {
		if c.Outcomes[i].HorizonMinutes == horizon && c.Outcomes[i].LabelVersion == lv {
			return &c.Outcomes[i]
		}
	}
	h := HorizonOutcomes{HorizonMinutes: horizon, LabelVersion: lv, ByEntryQuality: make([]QualityOutcome, len(entryQualityOrder))}
	for i, eq := range entryQualityOrder {
		h.ByEntryQuality[i].EntryQuality = eq
	}
	c.Outcomes = append(c.Outcomes, h)
	return &c.Outcomes[len(c.Outcomes)-1]
}
