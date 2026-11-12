package trialquery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Cap sources of Limits.OverallCapSource.
const (
	CapSourceDefault = "default"
	CapSourceStored  = "stored"
)

// settingsReader reads the jevtrialsettings keys only, with the same single-key
// statement the other fixed-key reads use.
type settingsReader struct{ q query.Q }

func (s settingsReader) Get(ctx context.Context, key string) (string, bool, error) {
	if !jevtrialsettings.IsKey(key) {
		return "", false, fmt.Errorf("query: runtime setting %q is not readable", key)
	}
	var value string
	err := s.q.QueryRowContext(ctx, `SELECT value FROM runtime_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query: runtime setting %s: %w", key, err)
	}
	return value, true, nil
}

// Cost reads the trial's cost of r (JST days, by created_at) by source, by arm
// and by day, and its current caps and stop state as of now. arms limit the
// per-arm and daily breakdown (empty: every arm with a row); the overall
// figures in Limits always cover every arm, since the overall cap does.
func Cost(ctx context.Context, q query.Q, r query.Range, now time.Time, arms []string) (CostReport, error) {
	if err := ValidateArms(arms); err != nil {
		return CostReport{}, err
	}
	from, to := r.Bounds()
	args := []any{from, to}
	filter := ""
	if len(arms) > 0 {
		filter = ` AND arm_id IN (` + placeholders(len(arms)) + `)`
		for _, a := range arms {
			args = append(args, a)
		}
	}
	rows, err := q.QueryContext(ctx, `
SELECT substr(datetime(created_at, '+9 hours'), 1, 10) AS day, arm_id, cost_source, COUNT(*),
	COALESCE(SUM(cost_usd), 0), COALESCE(SUM(counted_cost_usd), 0), COALESCE(SUM(counted_cost_usd = 0), 0)
FROM jev_trial_decisions WHERE created_at >= ? AND created_at < ?`+filter+`
GROUP BY day, arm_id, cost_source ORDER BY day, arm_id, cost_source`, args...)
	if err != nil {
		return CostReport{}, fmt.Errorf("query: jev trial cost: %w", err)
	}
	defer func() { _ = rows.Close() }()

	report := CostReport{Notice: CostNotice, Total: newCostBlock(), PerArm: []ArmCost{}, Daily: []DayCost{}}
	perArm := map[string]*CostBlock{}
	for rows.Next() {
		var (
			day, arm, source string
			calls, zero      int
			usd, counted     float64
		)
		if err := rows.Scan(&day, &arm, &source, &calls, &usd, &counted, &zero); err != nil {
			return CostReport{}, fmt.Errorf("query: scan jev trial cost: %w", err)
		}
		if len(report.Daily) == 0 || report.Daily[len(report.Daily)-1].Date != day {
			report.Daily = append(report.Daily, DayCost{Date: day, Overall: newCostBlock(), Arms: []ArmCost{}})
		}
		d := &report.Daily[len(report.Daily)-1]
		if len(d.Arms) == 0 || d.Arms[len(d.Arms)-1].ArmID != arm {
			d.Arms = append(d.Arms, ArmCost{ArmID: arm, CostBlock: newCostBlock()})
		}
		d.Arms[len(d.Arms)-1].add(source, calls, usd, counted, zero)
		d.Overall.add(source, calls, usd, counted, zero)
		report.Total.add(source, calls, usd, counted, zero)
		if perArm[arm] == nil {
			b := newCostBlock()
			perArm[arm] = &b
		}
		perArm[arm].add(source, calls, usd, counted, zero)
	}
	if err := rows.Err(); err != nil {
		return CostReport{}, fmt.Errorf("query: jev trial cost: %w", err)
	}
	ids := arms
	if len(ids) == 0 {
		for id := range perArm {
			ids = append(ids, id)
		}
	}
	for _, id := range slices.Sorted(slices.Values(slices.Clone(ids))) {
		b := newCostBlock()
		if p := perArm[id]; p != nil {
			b = *p
		}
		report.PerArm = append(report.PerArm, ArmCost{ArmID: id, CostBlock: b})
	}
	if report.Limits, err = readLimits(ctx, q, now, arms); err != nil {
		return CostReport{}, err
	}
	return report, nil
}

// readLimits reads the caps and stop state from runtime_settings and today's
// counted spend (overall and per arm) from jev_trial_decisions.
func readLimits(ctx context.Context, q query.Q, now time.Time, only []string) (Limits, error) {
	store := settingsReader{q}
	cfg := jevtrialsettings.Load(ctx, store)
	today := jevshadowsettings.Day(now)
	l := Limits{
		Enabled: cfg.Enabled, Today: today, ArmsConfigInvalid: cfg.ArmsError != "",
		OverallDailyCapUSD: cfg.CapUSD, OverallCapSource: CapSourceStored,
		FailureCap: jevtrialsettings.DailyFailureCap, ThrottleCooldownSeconds: int(jevtrialsettings.ThrottleCooldown.Seconds()),
		MinCallIntervalSeconds: int(jevtrialsettings.MinCallInterval.Seconds()), WorstCaseUSDPerMtok: jevtrialsettings.WorstCaseUSDPerMtok,
		Halt: haltInfo(cfg.Halt, today), Arms: []ArmLimits{},
	}
	if cfg.CapIsDefault {
		l.OverallCapSource = CapSourceDefault
	}
	from, to := jevshadowsettings.DayBounds(now)
	rows, err := q.QueryContext(ctx, `
SELECT arm_id, COALESCE(SUM(counted_cost_usd), 0) FROM jev_trial_decisions WHERE created_at >= ? AND created_at < ? GROUP BY arm_id`,
		sqlutil.FormatTime(from), sqlutil.FormatTime(to))
	if err != nil {
		return Limits{}, fmt.Errorf("query: today's jev trial cost: %w", err)
	}
	defer func() { _ = rows.Close() }()
	counted := map[string]float64{}
	for rows.Next() {
		var arm string
		var usd float64
		if err := rows.Scan(&arm, &usd); err != nil {
			return Limits{}, fmt.Errorf("query: scan today's jev trial cost: %w", err)
		}
		counted[arm] = usd
		l.OverallCountedTodayUSD += usd
	}
	if err := rows.Err(); err != nil {
		return Limits{}, fmt.Errorf("query: today's jev trial cost: %w", err)
	}
	for _, a := range cfg.Arms {
		if len(only) > 0 && !slices.Contains(only, a.ID) {
			continue
		}
		st := jevtrialsettings.LoadArm(ctx, store, a.ID)
		al := ArmLimits{
			ArmID: a.ID, Model: a.Model, PromptVersion: a.PromptVersion, DailyCapUSD: a.CapUSD(), ListPriceKnown: a.ListPriceUSDPerMtok > 0,
			CountedTodayUSD: counted[a.ID], FailuresToday: st.Failures.FailuresOn(today), Halt: haltInfo(st.Halt, today),
		}
		if t := st.Throttle; t != nil {
			al.Throttle = &ThrottleInfo{Date: t.Date, Reason: t.Reason, Active: t.ActiveAt(now)}
		}
		l.Arms = append(l.Arms, al)
	}
	return l, nil
}

func haltInfo(h *jevtrialsettings.Halt, today string) *HaltInfo {
	if h == nil {
		return nil
	}
	return &HaltInfo{Date: h.Date, Reason: h.Reason, ActiveToday: h.HaltOn(today)}
}
