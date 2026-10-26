package metalabeldiag

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// ExcludedVersion is what the retraining leaves out of one Trader prompt
// version of another cohort than the live one, within the decision window the
// readiness read (issue #913). A version with no trader decision in the window
// is not listed (it still is in excluded_prompt_versions).
type ExcludedVersion struct {
	PromptVersion string `json:"prompt_version"`
	Cohort        string `json:"cohort"`
	// TraderDecisions are all the version's trader decisions in the window,
	// LongShortRows the LONG/SHORT ones and LabelV2Rows those of them with a
	// triple-barrier (label_version 2) outcome: the rows the funnel would have
	// counted had the version not been left out.
	TraderDecisions int64 `json:"trader_decisions"`
	LongShortRows   int64 `json:"long_short_rows"`
	LabelV2Rows     int64 `json:"label_v2_rows"`
	// OldestDecisionDate and NewestDecisionDate are the JST dates of the
	// version's first and last trader decision in the window.
	OldestDecisionDate string `json:"oldest_decision_date"`
	NewestDecisionDate string `json:"newest_decision_date"`
}

// Promotion is the newest switch of the live prompt version
// (jev_prompt_promotions).
type Promotion struct {
	At          string `json:"at"`
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
}

// PromptCohort tells the rows a cohort left out (the live prompt's other
// cohort) apart from the rows the llm_cutoff moved to the OOS side: the funnel
// above counts only the live cohort's rows.
type PromptCohort struct {
	LivePromptVersion string            `json:"live_prompt_version"`
	LatestPromotion   *Promotion        `json:"latest_promotion"`
	Excluded          []ExcludedVersion `json:"excluded"`
}

// readPromptCohort reads the live version's cohort, the newest promotion and,
// for every excluded version, what it holds in [from, to).
func readPromptCohort(ctx context.Context, q query.Q, from, to time.Time, live string, foreign []string) (PromptCohort, error) {
	out := PromptCohort{LivePromptVersion: live, Excluded: []ExcludedVersion{}}
	var err error
	if out.LatestPromotion, err = latestPromotion(ctx, q); err != nil {
		return PromptCohort{}, err
	}
	if len(foreign) == 0 {
		return out, nil
	}
	f, t := query.Range{From: from, To: to}.Bounds()
	cond, args := sqlutil.In("d.question_version", foreign)
	rows, err := q.QueryContext(ctx, `
SELECT d.question_version, COUNT(*),
	COALESCE(SUM(d.direction IN ('LONG', 'SHORT')), 0),
	COALESCE(SUM(d.direction IN ('LONG', 'SHORT') AND EXISTS (
		SELECT 1 FROM calibration_outcomes o WHERE o.jev_decision_id = d.id AND o.label_version = ?)), 0),
	MIN(d.timestamp), MAX(d.timestamp)
FROM jev_decisions d
WHERE d.decision_type = 'trader' AND d.timestamp >= ? AND d.timestamp < ? AND `+cond+`
GROUP BY d.question_version ORDER BY MIN(d.timestamp), d.question_version`,
		append([]any{domain.CalibrationLabelVersionTripleBarrier, f, t}, args...)...)
	if err != nil {
		return PromptCohort{}, fmt.Errorf("query: excluded prompt versions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var e ExcludedVersion
		var oldest, newest string
		if err := rows.Scan(&e.PromptVersion, &e.TraderDecisions, &e.LongShortRows, &e.LabelV2Rows, &oldest, &newest); err != nil {
			return PromptCohort{}, fmt.Errorf("query: scan excluded prompt version: %w", err)
		}
		e.Cohort, _ = jev.TraderCohort(e.PromptVersion)
		if e.OldestDecisionDate, err = jstDate(oldest); err != nil {
			return PromptCohort{}, err
		}
		if e.NewestDecisionDate, err = jstDate(newest); err != nil {
			return PromptCohort{}, err
		}
		out.Excluded = append(out.Excluded, e)
	}
	if err := rows.Err(); err != nil {
		return PromptCohort{}, fmt.Errorf("query: excluded prompt versions: %w", err)
	}
	return out, nil
}

// latestPromotion is the newest jev_prompt_promotions row (nil: none).
func latestPromotion(ctx context.Context, q query.Q) (*Promotion, error) {
	var at, from, to string
	err := q.QueryRowContext(ctx, `SELECT at, from_version, to_version FROM jev_prompt_promotions ORDER BY id DESC LIMIT 1`).Scan(&at, &from, &to)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil
	case err != nil:
		return nil, fmt.Errorf("query: latest prompt promotion: %w", err)
	}
	return &Promotion{At: query.FormatOut(at), FromVersion: from, ToVersion: to}, nil
}

// jstDate is the JST date of a stored timestamp.
func jstDate(stored string) (string, error) {
	t, err := sqlutil.ParseTime(stored)
	if err != nil {
		return "", fmt.Errorf("query: %w", err)
	}
	return t.In(query.JST).Format(time.DateOnly), nil
}
