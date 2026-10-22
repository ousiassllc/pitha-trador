package query

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/domain/orderproposal"
)

// Model is one metalabel_history model (API spec §3.5). params_json,
// feature_names_json and oos_calibration_json are not selected.
type Model struct {
	Version               string   `json:"version"`
	Algorithm             string   `json:"algorithm"`
	LabelVersion          int      `json:"label_version"`
	Status                string   `json:"status"`
	RejectReason          string   `json:"reject_reason"`
	RejectReasonTruncated bool     `json:"reject_reason_truncated"`
	TrainedAt             string   `json:"trained_at"`
	TrainStart            string   `json:"train_start"`
	TrainEnd              string   `json:"train_end"`
	OosStart              string   `json:"oos_start"`
	OosEnd                string   `json:"oos_end"`
	TrainSamples          int64    `json:"train_samples"`
	OosSamples            int64    `json:"oos_samples"`
	OosAUC                *float64 `json:"oos_auc"`
	OosExpectedValueBps   *float64 `json:"oos_expected_value_bps"`
}

// PredictionCounts are the metalabel_predictions take/skip counts only.
type PredictionCounts struct {
	Take int `json:"take"`
	Skip int `json:"skip"`
}

// MetalabelHistory is metalabel_history's data.
type MetalabelHistory struct {
	Models           []Model          `json:"models"`
	PredictionCounts PredictionCounts `json:"prediction_counts"`
	// RetrainSkips are the nightly retrainings skipped for lack of data in
	// the period (newest first, up to the limit; issue #799).
	RetrainSkips []RetrainSkip `json:"retrain_skips"`
}

// Metalabel returns up to limit models trained in r (newest first), the
// take/skip counts of the predictions made in r and up to limit retrain skips.
func Metalabel(ctx context.Context, q Q, r Range, limit int) (MetalabelHistory, error) {
	from, to := r.bounds()
	rows, err := q.QueryContext(ctx, `
SELECT version, algorithm, label_version, status, reject_reason, trained_at,
	train_start, train_end, oos_start, oos_end, train_samples, oos_samples,
	oos_auc, oos_expected_value_bps
FROM metalabel_models
WHERE trained_at >= ? AND trained_at < ?
ORDER BY trained_at DESC, id DESC
LIMIT ?`, from, to, limit)
	if err != nil {
		return MetalabelHistory{}, fmt.Errorf("query: metalabel models: %w", err)
	}
	defer func() { _ = rows.Close() }()

	h := MetalabelHistory{Models: []Model{}}
	for rows.Next() {
		var (
			m                                                 Model
			trainedAt, trainStart, trainEnd, oosStart, oosEnd string
			auc, ev                                           sql.NullFloat64
		)
		if err := rows.Scan(&m.Version, &m.Algorithm, &m.LabelVersion, &m.Status, &m.RejectReason,
			&trainedAt, &trainStart, &trainEnd, &oosStart, &oosEnd,
			&m.TrainSamples, &m.OosSamples, &auc, &ev); err != nil {
			return MetalabelHistory{}, fmt.Errorf("query: scan metalabel model: %w", err)
		}
		m.TrainedAt, m.TrainStart, m.TrainEnd = FormatOut(trainedAt), FormatOut(trainStart), FormatOut(trainEnd)
		m.OosStart, m.OosEnd = FormatOut(oosStart), FormatOut(oosEnd)
		m.OosAUC, m.OosExpectedValueBps = floatPtr(auc), floatPtr(ev)
		m.RejectReason, m.RejectReasonTruncated = truncateRunes(m.RejectReason, MaxReasonRunes)
		h.Models = append(h.Models, m)
	}
	if err := rows.Err(); err != nil {
		return MetalabelHistory{}, fmt.Errorf("query: metalabel models: %w", err)
	}
	_ = rows.Close()

	err = q.QueryRowContext(ctx, `
SELECT COALESCE(SUM(action = 'take'), 0), COALESCE(SUM(action = 'skip'), 0)
FROM metalabel_predictions WHERE predicted_at >= ? AND predicted_at < ?`, from, to).
		Scan(&h.PredictionCounts.Take, &h.PredictionCounts.Skip)
	if err != nil {
		return MetalabelHistory{}, fmt.Errorf("query: metalabel prediction counts: %w", err)
	}
	if h.RetrainSkips, err = retrainSkips(ctx, q, r, limit); err != nil {
		return MetalabelHistory{}, err
	}
	return h, nil
}

// truncateRunes cuts s to max runes, reporting whether it cut.
func truncateRunes(s string, limit int) (string, bool) {
	if utf8.RuneCountInString(s) <= limit {
		return s, false
	}
	runes := []rune(s)
	return string(runes[:limit]), true
}

// RejectBucket is a Risk Engine rejection reason code and its count.
type RejectBucket struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// RiskRejections is risk_rejections' data (API spec §3.6). Reason text
// (counts, amounts, error messages) is never returned, only the code.
type RiskRejections struct {
	RiskPassedCount  int            `json:"risk_passed_count"`
	OtherRejectCount int            `json:"other_reject_count"`
	Buckets          []RejectBucket `json:"buckets"`
	// ProposalRiskPassedCount / ProposalRiskRejectedCount: how many of the
	// signals above came from order proposals (issue #828; policy_version is
	// orderproposal.PolicyVersion). A proposal's own rejection reasons never
	// reach trade_signals; they are in order_proposal_get.
	ProposalRiskPassedCount   int `json:"proposal_risk_passed_count"`
	ProposalRiskRejectedCount int `json:"proposal_risk_rejected_count"`
	// ProposalRejectedAfterRiskCount: proposal signals that passed the Risk
	// Engine but did not become an order (issue #860; size_zero, max_trade_loss,
	// ...). In neither risk_passed_count nor other_reject_count; the breakdown sums to it.
	ProposalRejectedAfterRiskCount     int            `json:"proposal_rejected_after_risk_count"`
	ProposalRejectedAfterRiskBreakdown []RejectBucket `json:"proposal_rejected_after_risk_breakdown"`
	// OtherRejectBreakdown splits other_reject_count (the Policy Engine's own
	// NONE reasons, not the Risk Engine's) by fixed reject_reason code, e.g.
	// jev_none or meta_skip:no_active_model; unlisted prefixes are "other" and
	// a missing reason "unknown". The counts sum to other_reject_count.
	OtherRejectBreakdown []RejectBucket `json:"other_reject_breakdown"`
}

// riskPrefix marks a Risk Engine rejection in trade_signals.reject_reason
// (internal/service/policy/engine.go).
const riskPrefix = "risk_engine_rejected:"

// Rejections counts the trade signals of r by Risk Engine rejection code. A
// proposal's signal that passed Risk but did not become an order (risk_passed
// = 0, post-risk prefix, issue #860) is only in proposal_rejected_after_risk_*.
func Rejections(ctx context.Context, q Q, r Range) (RiskRejections, error) {
	from, to := r.bounds()
	out := RiskRejections{}
	err := q.QueryRowContext(ctx, `
SELECT COALESCE(SUM(risk_passed <> 0), 0),
	COALESCE(SUM(risk_passed = 0 AND `+otherRejectReason+`), 0),
	COALESCE(SUM(risk_passed <> 0 AND policy_version = ?), 0),
	COALESCE(SUM(risk_passed = 0 AND policy_version = ? AND reject_reason GLOB '`+riskPrefix+`*'), 0),
	COALESCE(SUM(risk_passed = 0 AND reject_reason GLOB '`+orderproposal.PostRiskRejectPrefix+`*'), 0)
FROM trade_signals WHERE timestamp >= ? AND timestamp < ?`,
		orderproposal.PolicyVersion, orderproposal.PolicyVersion, from, to).
		Scan(&out.RiskPassedCount, &out.OtherRejectCount, &out.ProposalRiskPassedCount, &out.ProposalRiskRejectedCount, &out.ProposalRejectedAfterRiskCount)
	if err != nil {
		return RiskRejections{}, fmt.Errorf("query: risk rejection totals: %w", err)
	}
	if out.Buckets, err = reasonBuckets(ctx, q, from, to, riskPrefix); err != nil {
		return RiskRejections{}, err
	}
	if out.ProposalRejectedAfterRiskBreakdown, err = reasonBuckets(ctx, q, from, to, orderproposal.PostRiskRejectPrefix); err != nil {
		return RiskRejections{}, err
	}
	if out.OtherRejectBreakdown, err = otherRejectBreakdown(ctx, q, from, to); err != nil {
		return RiskRejections{}, err
	}
	return out, nil
}

// reasonBuckets counts the risk_passed = 0 signals whose reject_reason starts
// with prefix by reason code (text up to the next ':', trimmed; empty is
// "unknown"), most frequent first. Grouping is in SQL: detail text stays there.
func reasonBuckets(ctx context.Context, q Q, from, to, prefix string) ([]RejectBucket, error) {
	rows, err := q.QueryContext(ctx, `
SELECT CASE WHEN token = '' THEN 'unknown' ELSE token END AS reason, COUNT(*) FROM (
	SELECT trim(CASE WHEN instr(rest, ':') > 0 THEN substr(rest, 1, instr(rest, ':') - 1) ELSE rest END,
		' '||char(9)||char(10)||char(13)) AS token
	FROM (SELECT substr(reject_reason, ?) AS rest FROM trade_signals
		WHERE timestamp >= ? AND timestamp < ? AND risk_passed = 0 AND reject_reason GLOB ?)
) GROUP BY reason`, len(prefix)+1, from, to, prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("query: rejection buckets of %q: %w", prefix, err)
	}
	defer func() { _ = rows.Close() }()
	buckets := []RejectBucket{}
	for rows.Next() {
		var b RejectBucket
		if err = rows.Scan(&b.Reason, &b.Count); err != nil {
			return nil, fmt.Errorf("query: scan rejection bucket of %q: %w", prefix, err)
		}
		buckets = append(buckets, b)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("query: rejection buckets of %q: %w", prefix, err)
	}
	sortBuckets(buckets)
	return buckets, nil
}

// sortBuckets orders buckets by count, descending, then by reason.
func sortBuckets(b []RejectBucket) {
	slices.SortFunc(b, func(x, y RejectBucket) int { return cmp.Or(y.Count-x.Count, strings.Compare(x.Reason, y.Reason)) })
}

// otherRejectReason: the Policy Engine's own rejection (neither Risk's nor post-risk).
const otherRejectReason = `(reject_reason IS NULL OR NOT (reject_reason GLOB '` + riskPrefix + `*'
	OR reject_reason GLOB '` + orderproposal.PostRiskRejectPrefix + `*'))`

// KillSwitchEvent is one kill_switch_history row (API spec §3.7).
// detail_json is not selected.
type KillSwitchEvent struct {
	ID          int64   `json:"id"`
	TriggeredAt string  `json:"triggered_at"`
	Reason      string  `json:"reason"`
	ResolvedAt  *string `json:"resolved_at"`
	ResolvedBy  *string `json:"resolved_by"`
}

// KillSwitchHistory returns up to limit events triggered in r, newest first,
// joined to their resolution.
func KillSwitchHistory(ctx context.Context, q Q, r Range, limit int) ([]KillSwitchEvent, error) {
	from, to := r.bounds()
	rows, err := q.QueryContext(ctx, `
SELECT e.id, e.triggered_at, e.reason, s.resolved_at, s.resolved_by
FROM kill_switch_events e
LEFT JOIN kill_switch_resolutions s ON s.kill_switch_event_id = e.id
WHERE e.triggered_at >= ? AND e.triggered_at < ?
ORDER BY e.triggered_at DESC, e.id DESC
LIMIT ?`, from, to, limit)
	if err != nil {
		return nil, fmt.Errorf("query: kill switch events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := []KillSwitchEvent{}
	for rows.Next() {
		var (
			e          KillSwitchEvent
			triggered  string
			resolvedAt sql.NullString
			resolvedBy sql.NullString
		)
		if err := rows.Scan(&e.ID, &triggered, &e.Reason, &resolvedAt, &resolvedBy); err != nil {
			return nil, fmt.Errorf("query: scan kill switch event: %w", err)
		}
		e.TriggeredAt = FormatOut(triggered)
		e.ResolvedAt = formatOutPtr(resolvedAt)
		if resolvedBy.Valid {
			e.ResolvedBy = &resolvedBy.String
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("query: kill switch events: %w", err)
	}
	return events, nil
}
