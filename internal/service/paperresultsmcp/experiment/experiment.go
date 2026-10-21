// Package experiment is the tuning-experiment vocabulary of the Paper results
// MCP (issue #871): what an experiment is (one hypothesis, ONE changed
// variable, a start and an end), how an experiment_create request is
// validated, the SQL that stores and reads tuning_experiments (store.go), and
// the before / after comparison with its verdict (compare.go).
//
// The comparison reuses query.PerformanceSummary and query.Rejections, the
// functions behind paper_performance_summary and risk_rejections, so its
// figures are those tools' figures for the same period; there is no second
// calculation. Writing is mcpset's job: the MCP writer (experiment_create /
// experiment_close, behind the write switch) and the application's Gate (the
// Settings screen).
package experiment

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
)

// Kinds of the one variable an experiment changes.
const (
	// KindSetting is a key of mcpwritesettings.Keys (the whitelist).
	KindSetting = "setting"
	// KindModel is the Jev model (jev_decisions.model_id).
	KindModel = "model"
	// KindPrompt is the Jev prompt version (jev_decisions.question_version).
	KindPrompt = "prompt"
)

// Statuses of an experiment. Only running has no end time.
const (
	StatusRunning = "running"
	StatusClosed  = "closed"
	// StatusAborted is an experiment given up before it finished: it is never judged.
	StatusAborted = "aborted"
)

// Actors of an experiment, the same words as mcp_settings_audit.actor.
const (
	ActorMCP = "mcp"
	ActorGUI = "gui"
)

// Limits of the text columns (the CHECKs of migration 000047).
const (
	MaxHypothesisRunes = 500
	MaxNameRunes       = 200
	MaxValueRunes      = 200
)

var (
	// ErrMultipleVariables is returned for a request that changes more than
	// one variable. Nothing was stored.
	ErrMultipleVariables = errors.New("experiment: an experiment changes exactly one variable")
	// ErrKeyNotAllowed is returned for a setting that is not on the
	// whitelist (mcpwritesettings.Keys): a conclusion about it could not be
	// checked for confounders. Nothing was stored.
	ErrKeyNotAllowed = errors.New("experiment: the setting is not on the MCP whitelist")
	// ErrNotFound is returned for an unknown experiment id.
	ErrNotFound = errors.New("experiment: not found")
	// ErrNotRunning is returned when closing an experiment that already ended.
	ErrNotRunning = errors.New("experiment: not running")
)

// InvalidError is a validation failure. Reason names the argument and the rule
// and never quotes a value, so it is safe to show to the caller.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "experiment: " + e.Reason }

func invalid(format string, a ...any) error {
	return &InvalidError{Reason: fmt.Sprintf(format, a...)}
}

// IsInvalid reports whether err is an *InvalidError and returns its reason.
func IsInvalid(err error) (reason string, ok bool) {
	var ie *InvalidError
	if errors.As(err, &ie) {
		return ie.Reason, true
	}
	return "", false
}

// Change is one changed variable as the caller states it.
type Change struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// Args are the arguments of experiment_create (and the Settings form) before
// validation. Changes is a list only so that a request naming two variables is
// refused with its own error instead of being silently cut to one.
type Args struct {
	Hypothesis string
	Changes    []Change
	// StartedAt is RFC3339; empty means now.
	StartedAt string
	// AuditID is the mcp_settings_audit row of the change (kind setting only).
	AuditID *int64
}

// Request is a validated experiment: text trimmed, StartedAt resolved.
type Request struct {
	Hypothesis string
	Change     Change
	StartedAt  time.Time
	AuditID    *int64
}

// Validate checks a against the rules of issue #871 and returns the request.
// The order: exactly one variable (ErrMultipleVariables), the hypothesis, the
// variable's kind / name / values, the audit id, the start time (not in the
// future: an experiment records a change that has already taken effect).
func (a Args) Validate(now time.Time) (Request, error) {
	switch len(a.Changes) {
	case 0:
		return Request{}, invalid("changes には変える変数を 1 つ指定してください")
	case 1:
	default:
		return Request{}, ErrMultipleVariables
	}
	hypothesis, err := cleanText(a.Hypothesis, "hypothesis", MaxHypothesisRunes, true)
	if err != nil {
		return Request{}, err
	}
	c := a.Changes[0]
	switch c.Kind {
	case KindSetting, KindModel, KindPrompt:
	default:
		return Request{}, invalid("kind は %s / %s / %s のどれかです", KindSetting, KindModel, KindPrompt)
	}
	if c.Name, err = cleanText(c.Name, "name", MaxNameRunes, false); err != nil {
		return Request{}, err
	}
	if c.Kind == KindSetting && !mcpwritesettings.IsAllowed(c.Name) {
		return Request{}, ErrKeyNotAllowed
	}
	if c.Before, err = cleanText(c.Before, "before", MaxValueRunes, false); err != nil {
		return Request{}, err
	}
	if c.After, err = cleanText(c.After, "after", MaxValueRunes, false); err != nil {
		return Request{}, err
	}
	if c.Before == c.After {
		return Request{}, invalid("before と after が同じです（変えていない実験は作れません）")
	}
	if a.AuditID != nil && (c.Kind != KindSetting || *a.AuditID < 1) {
		return Request{}, invalid("audit_id は kind が setting のときだけ、1 以上の監査 ID を指定できます")
	}
	started := now
	if a.StartedAt != "" {
		if started, err = time.Parse(time.RFC3339, a.StartedAt); err != nil {
			return Request{}, invalid("started_at は RFC3339 の時刻です")
		}
		if started.After(now) {
			return Request{}, invalid("started_at は現在より後にできません")
		}
	}
	return Request{Hypothesis: hypothesis, Change: c, StartedAt: started.UTC(), AuditID: a.AuditID}, nil
}

// cleanText trims s and checks it is non-empty, at most max characters and
// free of control characters (a newline is allowed when multiline).
func cleanText(s, field string, max int, multiline bool) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case s == "":
		return "", invalid("%s が空です", field)
	case utf8.RuneCountInString(s) > max:
		return "", invalid("%s は %d 文字以内です", field, max)
	}
	for _, r := range s {
		if unicode.IsControl(r) && (!multiline || r != '\n') {
			return "", invalid("%s に制御文字は使えません", field)
		}
	}
	return s, nil
}

// Variable is the one thing an experiment changed.
type Variable struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Before string `json:"before"`
	After  string `json:"after"`
	// AuditID is the mcp_settings_audit row of a setting change, when known.
	AuditID *int64 `json:"audit_id"`
}

// Experiment is one stored tuning_experiments row. The times are UTC RFC3339
// in the JSON; the exact instants (startedAt, endedAt) are kept for the
// before / after windows.
type Experiment struct {
	ID         int64    `json:"id"`
	CreatedAt  string   `json:"created_at"`
	Actor      string   `json:"actor"`
	Client     string   `json:"client"`
	Hypothesis string   `json:"hypothesis"`
	Variable   Variable `json:"variable"`
	StartedAt  string   `json:"started_at"`
	EndedAt    *string  `json:"ended_at"`
	Status     string   `json:"status"`

	startedAt time.Time
	endedAt   *time.Time
}

// Window is the half-open period [start, end) the experiment's "after" covers:
// from its start to its end, or to now while it runs.
func (e Experiment) window(now time.Time) (start, end time.Time) {
	start, end = e.startedAt, now
	if e.endedAt != nil {
		end = *e.endedAt
	}
	if end.Before(start) {
		end = start
	}
	return start, end
}
