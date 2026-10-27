package query

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/apierror"
)

// Codes of the non-risk rejections risk_rejections' other_reject_breakdown may
// return (issue #800). They are the fixed reject_reason prefixes of
// internal/service/policy (engine.go, meta.go, direction.go, regime.go); a
// reason this build does not know is "other", and a missing one "unknown".
const (
	rejectCodeOther   = "other"
	rejectCodeUnknown = "unknown"
	metaSkipHead      = "meta_skip"
	apiErrorHead      = "api_error"
	jevNoneHead       = "jev_none"
)

// The codes of the jev_none rows (issue #922). A row that has the confidence
// and the regime of Jev's answer ("jev_none: confidence=0.8125 regime=RANGE",
// issue #912) is counted twice: once by regime and once by confidence band,
// never by both together. A row from before #912 (no "confidence=") stays
// "jev_none", and a row whose regime is not a known one or whose confidence is
// not a number in [0, 1] is "jev_none:other"; those two are in both views.
// So the codes that are not "jev_none:conf=..." add up to other_reject_count,
// and so do those that are not "jev_none:regime=...".
const (
	jevNoneOther        = jevNoneHead + ":" + rejectCodeOther
	jevNoneRegimePrefix = jevNoneHead + ":regime="
	jevNoneConfPrefix   = jevNoneHead + ":conf="

	// Edges of the confidence bands (Jev's confidence is in [0, 1]); a value
	// on an edge is in the band above it.
	confBandLow  = "lt_0.4"
	confBandMid  = "0.4_to_0.6"
	confBandHigh = "0.6_to_0.8"
	confBandTop  = "ge_0.8"

	// What the SQL says of a jev_none row: legacy (no confidence= at all),
	// parsed (a confidence in [0, 1] and a known regime) or other.
	jevFormLegacy = "legacy"
	jevFormParsed = "parsed"
	jevFormOther  = "other"
)

// jevRegimes are the regimes of Jev's answer (domain.JevRegime*); any other
// name is not passed on.
var jevRegimes = []string{domain.JevRegimeTrend, domain.JevRegimeRange, domain.JevRegimeBreakout, domain.JevRegimeChaotic}

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

// apiErrorKinds are the codes the Policy Engine writes after "api_error:"
// (internal/service/policy/apierror.Codes, issue #912). An older row, which has
// the error text there, or a word this build does not know stays plain api_error.
var apiErrorKinds = func() map[string]bool {
	kinds := map[string]bool{}
	for _, kind := range apierror.Codes {
		kinds[kind] = true
	}
	return kinds
}()

// RejectCode maps a reject_reason's head (the text before the first ':') and,
// for meta_skip and api_error, its first word after it to the fixed code
// returned to the client: the head itself, "meta_skip:<reason>" for a known skip
// reason, "api_error:<kind>" for a known failure kind (plain "api_error" without
// one), "unknown" for no reason at all and "other" for anything else. Detail text
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
	case head == apiErrorHead:
		if apiErrorKinds[sub] {
			return apiErrorHead + ":" + sub
		}
		return apiErrorHead // no recorded kind: a row from before issue #912 stays where it was counted
	case otherRejectHeads[head]:
		return head
	}
	return rejectCodeOther
}

// otherRejectBreakdown counts the non-risk rejections of r by RejectCode, and
// splits jev_none by regime and by confidence band (see jevNoneOther). The
// head, the meta_skip word and the jev_none fields are cut and checked in SQL,
// so no detail text, regime name or confidence leaves the database as it was
// written; the counts of each view add up to other_reject_count because both
// use the same predicate and every row is in exactly one code of a view.
func otherRejectBreakdown(ctx context.Context, q Q, from, to string) ([]RejectBucket, error) {
	args := make([]any, 0, len(jevRegimes)+2)
	for _, regime := range jevRegimes {
		args = append(args, regime)
	}
	args = append(args, from, to)
	rows, err := q.QueryContext(ctx, `
SELECT head,
	CASE WHEN head IN ('meta_skip', 'api_error') THEN
		CASE WHEN instr(rest, ' ') > 0 THEN substr(rest, 1, instr(rest, ' ') - 1) ELSE rest END
	ELSE '' END AS sub,
	form,
	CASE WHEN form = '`+jevFormParsed+`' THEN rtok ELSE '' END AS regime,
	CASE WHEN form = '`+jevFormParsed+`' THEN
		CASE WHEN CAST(ctok AS REAL) < 0.4 THEN '`+confBandLow+`'
			WHEN CAST(ctok AS REAL) < 0.6 THEN '`+confBandMid+`'
			WHEN CAST(ctok AS REAL) < 0.8 THEN '`+confBandHigh+`'
			ELSE '`+confBandTop+`' END
	ELSE '' END AS band,
	COUNT(*)
FROM (
	SELECT head, rest, ctok, rtok,
		CASE WHEN head <> '`+jevNoneHead+`' THEN ''
			WHEN instr(rest, 'confidence=') = 0 THEN '`+jevFormLegacy+`'
			WHEN ctok <> '' AND ctok GLOB '[0-9]*' AND ctok NOT GLOB '*[^0-9.]*' AND length(ctok) - length(replace(ctok, '.', '')) <= 1
				AND CAST(ctok AS REAL) <= 1 AND rtok IN (`+strings.TrimSuffix(strings.Repeat("?, ", len(jevRegimes)), ", ")+`)
				THEN '`+jevFormParsed+`'
			ELSE '`+jevFormOther+`' END AS form
	FROM (
		-- confidence=<token> regime=<token>: each token up to the next space (the regime also up to ';').
		SELECT head, rest, ctok,
			CASE WHEN instr(rreg, ' ') > 0 THEN substr(rreg, 1, instr(rreg, ' ') - 1) ELSE rreg END AS rtok
		FROM (
			SELECT head, rest, ctok,
				CASE WHEN substr(rtail, 1, 7) = 'regime=' THEN replace(substr(rtail, 8), ';', ' ') ELSE '' END AS rreg
			FROM (
				SELECT head, rest,
					CASE WHEN instr(ctail, ' ') > 0 THEN substr(ctail, 1, instr(ctail, ' ') - 1) ELSE ctail END AS ctok,
					CASE WHEN instr(ctail, ' ') > 0 THEN ltrim(substr(ctail, instr(ctail, ' ') + 1), ' ') ELSE '' END AS rtail
				FROM (
					SELECT head, rest,
						CASE WHEN substr(rest, 1, 11) = 'confidence=' THEN substr(rest, 12) ELSE '' END AS ctail
					FROM (
						SELECT trim(CASE WHEN instr(reason, ':') > 0 THEN substr(reason, 1, instr(reason, ':') - 1) ELSE reason END,
								' '||char(9)||char(10)||char(13)) AS head,
							trim(CASE WHEN instr(reason, ':') > 0 THEN substr(reason, instr(reason, ':') + 1) ELSE '' END,
								' '||char(9)||char(10)||char(13)) AS rest
						FROM (
							SELECT COALESCE(reject_reason, '') AS reason
							FROM trade_signals
							WHERE timestamp >= ? AND timestamp < ? AND risk_passed = 0
								AND `+otherRejectReason+`
						)
					)
				)
			)
		)
	)
)
GROUP BY head, sub, form, regime, band`, args...)
	if err != nil {
		return nil, fmt.Errorf("query: other rejection breakdown: %w", err)
	}
	defer func() { _ = rows.Close() }()

	shared := map[string]int{} // in every view
	byRegime := map[string]int{}
	byConf := map[string]int{}
	for rows.Next() {
		var (
			head, sub, form, regime, band string
			n                             int
		)
		if err := rows.Scan(&head, &sub, &form, &regime, &band, &n); err != nil {
			return nil, fmt.Errorf("query: scan other rejection bucket: %w", err)
		}
		switch {
		case head != jevNoneHead:
			shared[RejectCode(head, sub)] += n
		case form == jevFormLegacy:
			shared[jevNoneHead] += n
		case form == jevFormParsed && slices.Contains(jevRegimes, regime):
			byRegime[jevNoneRegimePrefix+regime] += n
			byConf[jevNoneConfPrefix+band] += n
		default:
			shared[jevNoneOther] += n
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: other rejection breakdown: %w", err)
	}
	out := make([]RejectBucket, 0, len(shared)+len(byRegime)+len(byConf))
	for _, counts := range []map[string]int{shared, byRegime, byConf} {
		for code, n := range counts {
			out = append(out, RejectBucket{Reason: code, Count: n})
		}
	}
	sortBuckets(out)
	return out, nil
}
