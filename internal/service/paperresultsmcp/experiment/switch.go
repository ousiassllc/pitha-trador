package experiment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/sqlutil"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Whether the variable the experiment declares really went from its declared
// before to its declared after (issue #879). The comparison says nothing when
// the "change" did not happen as declared: a setting that never moved, a model
// that was the same on both sides, a start time that is nowhere near the
// switch. Only SwitchConfirmed lets the verdict be judged; the other three
// withhold it (VerdictSwitchUnconfirmed) and say which way it failed.
const (
	// SwitchConfirmed: a record shows the switch from before to after, at the
	// declared start.
	SwitchConfirmed = "confirmed"
	// SwitchMismatch: a record shows a switch, but not the declared one (other
	// values, or at a time far from started_at, so one of the two periods ran
	// on both values).
	SwitchMismatch = "mismatch"
	// SwitchNotSwitched: the record shows no change of the variable (the same
	// value saved again, the same model on both sides).
	SwitchNotSwitched = "not_switched"
	// SwitchUnverifiable: nothing on record can confirm or refute the switch
	// (no audit row of the setting; no Jev decision in a period).
	SwitchUnverifiable = "unverifiable"
)

// StartTolerance is how far the recorded switch may lie from the declared
// started_at (before or after it) and still be taken as "the change that
// started the experiment". started_at is when the change took effect; the
// Settings form takes it to the minute, and a model switch is saved some time
// before the application restarts on it. Further than that, the before period
// (or the after period) would be a stretch that ran on the other value, and
// the comparison would not be between before and after.
const StartTolerance = 15 * time.Minute

// SwitchCheck is the outcome of the check. Detail is a fixed sentence that
// never quotes a value.
type SwitchCheck struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// Confirmed reports whether the switch was confirmed.
func (s SwitchCheck) Confirmed() bool { return s.Status == SwitchConfirmed }

func confirmed() SwitchCheck {
	return SwitchCheck{Status: SwitchConfirmed, Detail: "宣言した変更前→変更後の切り替えが、記録で確認できました。"}
}

// checkSwitch verifies the experiment's variable against what is on record.
// seen are the Jev values of the before and after periods that confounders
// already read.
//
// A setting is read from mcp_settings_audit: the row of the experiment's
// audit_id, or, when it was omitted, the newest row of the key that changed
// the value (a same-value save is not a switch) saved no later than
// started_at + StartTolerance. Its before / after must be the declared ones
// (a NULL side - "no stored value, the default applies" - cannot be compared
// with a declared value and is not held against it) and it must have been
// saved within StartTolerance of started_at.
//
// A model or a prompt version is read from jev_decisions (the model_id /
// question_version the decisions recorded): the declared before must have been
// used in the before period and the declared after in the after period, the
// after value must not have been in use earlier than started_at -
// StartTolerance, and the before value not later than started_at +
// StartTolerance. A period with no decision cannot confirm anything.
func checkSwitch(ctx context.Context, q query.Q, exp Experiment, before, after query.Range, seen [2]jevValues) (SwitchCheck, error) {
	switch exp.Variable.Kind {
	case KindSetting:
		return checkSettingSwitch(ctx, q, exp)
	case KindModel:
		return checkJevSwitch(ctx, q, exp, "model_id", before, after, seen[0].models, seen[1].models)
	case KindPrompt:
		return checkJevSwitch(ctx, q, exp, "question_version", before, after, seen[0].prompts, seen[1].prompts)
	}
	return SwitchCheck{Status: SwitchUnverifiable, Detail: "変数の種類が不明で、切り替えを確認できません。"}, nil
}

func checkSettingSwitch(ctx context.Context, q query.Q, exp Experiment) (SwitchCheck, error) {
	v := exp.Variable
	var (
		at            string
		before, after *string
	)
	if v.AuditID != nil {
		err := q.QueryRowContext(ctx, `
SELECT at, before_value, after_value FROM mcp_settings_audit WHERE id = ? AND setting_key = ? AND action IN ('set', 'reset')`,
			*v.AuditID, v.Name).Scan(&at, &before, &after)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			return SwitchCheck{Status: SwitchMismatch, Detail: "audit_id の行が、この設定の変更の記録として見つかりません。"}, nil
		case err != nil:
			return SwitchCheck{}, fmt.Errorf("experiment: read switch audit row %d: %w", *v.AuditID, err)
		}
	} else {
		rows, err := q.QueryContext(ctx, `
SELECT at, before_value, after_value FROM mcp_settings_audit
WHERE setting_key = ? AND action IN ('set', 'reset') AND at <= ? ORDER BY at DESC, id DESC`,
			v.Name, sqlutil.FormatTime(exp.startedAt.Add(StartTolerance)))
		if err != nil {
			return SwitchCheck{}, fmt.Errorf("experiment: read switch audit rows: %w", err)
		}
		defer func() { _ = rows.Close() }()
		found := false
		for rows.Next() {
			if err := rows.Scan(&at, &before, &after); err != nil {
				return SwitchCheck{}, fmt.Errorf("experiment: scan switch audit row: %w", err)
			}
			if !noChange(before, after) {
				found = true
				break
			}
		}
		if err := rows.Err(); err != nil {
			return SwitchCheck{}, fmt.Errorf("experiment: read switch audit rows: %w", err)
		}
		if !found {
			return SwitchCheck{Status: SwitchUnverifiable, Detail: "この設定を変更した記録（監査ログ）が開始時刻の前後に無く、切り替えを確認できません。"}, nil
		}
	}
	switch {
	case noChange(before, after):
		return SwitchCheck{Status: SwitchNotSwitched, Detail: "audit_id の行は値を変えていません（同じ値の保存し直しです）。"}, nil
	case !matches(before, v.Before) || !matches(after, v.After):
		return SwitchCheck{Status: SwitchMismatch, Detail: "記録された変更が、宣言した変更前→変更後の値と違います。"}, nil
	}
	saved, err := sqlutil.ParseTime(at)
	if err != nil {
		return SwitchCheck{}, fmt.Errorf("experiment: %w", err)
	}
	if saved.Sub(exp.startedAt).Abs() > StartTolerance {
		return SwitchCheck{Status: SwitchMismatch, Detail: fmt.Sprintf("変更が保存された時刻が started_at から %d 分以上離れています（前後どちらかの期間が、もう一方の値で動いた時間を含みます）。", int(StartTolerance.Minutes()))}, nil
	}
	return confirmed(), nil
}

// checkJevSwitch is the model / prompt side of checkSwitch; column is
// jev_decisions.model_id or question_version and beforeSeen / afterSeen the
// values the two periods' decisions carried, by decision type.
func checkJevSwitch(ctx context.Context, q query.Q, exp Experiment, column string, before, after query.Range, beforeSeen, afterSeen map[string][]string) (SwitchCheck, error) {
	v := exp.Variable
	b, a := flatten(beforeSeen), flatten(afterSeen)
	switch {
	case len(b) == 0 || len(a) == 0:
		return SwitchCheck{Status: SwitchUnverifiable, Detail: "前または後の期間に Jev の判断の記録が無く、切り替えを確認できません。"}, nil
	case len(b) == 1 && len(a) == 1 && b[0] == a[0]:
		return SwitchCheck{Status: SwitchNotSwitched, Detail: "前の期間と後の期間で、判断に使われた値が同じです（切り替わっていません）。"}, nil
	case !slices.Contains(b, v.Before) || !slices.Contains(a, v.After):
		return SwitchCheck{Status: SwitchMismatch, Detail: "前の期間に変更前の値、後の期間に変更後の値が、判断の記録に見つかりません。"}, nil
	}
	start := exp.startedAt
	for _, c := range []struct {
		value, detail string
		from, to      time.Time
	}{
		{v.After, "変更後の値が、開始よりずっと前から使われています（前の期間の一部が変更後の値で動いています）。", before.From, start.Add(-StartTolerance)},
		{v.Before, "変更前の値が、開始のあとも使われ続けています（後の期間の一部が変更前の値で動いています）。", start.Add(StartTolerance), after.To},
	} {
		var n int
		err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM jev_decisions WHERE `+column+` = ? AND timestamp >= ? AND timestamp < ?`,
			c.value, sqlutil.FormatTime(c.from), sqlutil.FormatTime(c.to)).Scan(&n)
		if err != nil {
			return SwitchCheck{}, fmt.Errorf("experiment: read switch decisions: %w", err)
		}
		if n > 0 {
			return SwitchCheck{Status: SwitchMismatch, Detail: c.detail}, nil
		}
	}
	return confirmed(), nil
}

// flatten is every distinct value of a by-decision-type map.
func flatten(byType map[string][]string) []string {
	var out []string
	for _, values := range byType {
		for _, v := range values {
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// noChange reports whether an audit row left the value as it was: the same
// value stored again (as text or as a number), or a reset of a key with no
// stored value. A NULL side means "no stored value, the default applies", so a
// row between a NULL and a value is a change.
func noChange(before, after *string) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	return sameValue(*before, *after)
}

// matches compares a stored audit value with a declared one. A NULL stored
// value (the default applied) cannot be compared and is not held against the
// declaration.
func matches(stored *string, declared string) bool {
	return stored == nil || sameValue(*stored, declared)
}
