package query

import (
	"context"
	"fmt"
	"sort"
)

// Codes of the non-risk rejections risk_rejections' other_reject_breakdown may
// return (issue #800). They are the fixed reject_reason prefixes of
// internal/service/policy (engine.go, meta.go, direction.go, regime.go); a
// reason this build does not know is "other", and a missing one "unknown".
const (
	rejectCodeOther   = "other"
	rejectCodeUnknown = "unknown"
	metaSkipHead      = "meta_skip"
)

// otherRejectHeads are the policy reject_reason prefixes (the text before the
// first ':') reported as themselves. risk_engine_rejected is not among them:
// those rows are the buckets.
var otherRejectHeads = map[string]bool{
	"jev_none":                                 true,
	"probability_below_threshold":              true,
	"entry_quality_below_threshold":            true,
	"continuation_probability_below_threshold": true,
	"toxic_flow_above_threshold":               true,
	"liquidity_stressed_above_threshold":       true,
	"spread_too_wide":                          true,
	"thin_liquidity":                           true,
	"special_quote":                            true,
	"price_limit":                              true,
	"not_lendable":                             true,
	"missing_data":                             true,
	"api_error":                                true,
	"not_calibrated":                           true,
	"meta_model_unavailable":                   true,
	"vwap_z_unavailable":                       true,
	"vwap_not_extreme":                         true,
	metaSkipHead:                               true,
}

// metaSkipReasons are the internal/service/metalabel Decision reasons that
// follow "meta_skip: ".
var metaSkipReasons = map[string]bool{
	"no_active_model":      true,
	"model_unusable":       true,
	"no_candidate":         true,
	"missing_feature":      true,
	"invalid_config":       true,
	"predict_failed":       true,
	"ev_not_above_minimum": true,
}

// RejectCode maps a reject_reason's head (the text before the first ':') and,
// for meta_skip, its first word after it to the fixed code returned to the
// client: the head itself, "meta_skip:<reason>" for a known skip reason,
// "unknown" for no reason at all and "other" for anything else. Detail text
// (numbers, error messages) is never part of a code.
func RejectCode(head, sub string) string {
	switch {
	case head == "":
		return rejectCodeUnknown
	case head == metaSkipHead:
		if metaSkipReasons[sub] {
			return metaSkipHead + ":" + sub
		}
		return metaSkipHead + ":" + rejectCodeOther
	case otherRejectHeads[head]:
		return head
	}
	return rejectCodeOther
}

// otherRejectBreakdown counts the non-risk rejections of r by RejectCode. The
// head and meta_skip word are cut in SQL, so no detail text leaves the
// database; the counts add up to other_reject_count because both use the same
// predicate.
func otherRejectBreakdown(ctx context.Context, q Q, from, to string) ([]RejectBucket, error) {
	rows, err := q.QueryContext(ctx, `
SELECT head,
	CASE WHEN head = 'meta_skip' THEN
		CASE WHEN instr(rest, ' ') > 0 THEN substr(rest, 1, instr(rest, ' ') - 1) ELSE rest END
	ELSE '' END AS sub,
	COUNT(*)
FROM (
	SELECT trim(CASE WHEN instr(reason, ':') > 0 THEN substr(reason, 1, instr(reason, ':') - 1) ELSE reason END,
			' '||char(9)||char(10)||char(13)) AS head,
		trim(CASE WHEN instr(reason, ':') > 0 THEN substr(reason, instr(reason, ':') + 1) ELSE '' END,
			' '||char(9)||char(10)||char(13)) AS rest
	FROM (
		SELECT COALESCE(reject_reason, '') AS reason
		FROM trade_signals
		WHERE timestamp >= ? AND timestamp < ? AND risk_passed = 0
			AND (reject_reason IS NULL OR NOT reject_reason GLOB 'risk_engine_rejected:*')
	)
)
GROUP BY head, sub`, from, to)
	if err != nil {
		return nil, fmt.Errorf("query: other rejection breakdown: %w", err)
	}
	defer func() { _ = rows.Close() }()

	counts := map[string]int{}
	for rows.Next() {
		var (
			head, sub string
			n         int
		)
		if err := rows.Scan(&head, &sub, &n); err != nil {
			return nil, fmt.Errorf("query: scan other rejection bucket: %w", err)
		}
		counts[RejectCode(head, sub)] += n
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: other rejection breakdown: %w", err)
	}
	out := make([]RejectBucket, 0, len(counts))
	for code, n := range counts {
		out = append(out, RejectBucket{Reason: code, Count: n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Reason < out[j].Reason
	})
	return out, nil
}
