// Package researchnote is the research-note vocabulary of the Paper results MCP
// (issue #1095): what a note is (a question, a hypothesis, what was done, a
// result, a conclusion, links to the evidence and tags), how a write is
// validated and masked, and the SQL that stores it (store.go) and reads it
// (read.go).
//
// A note is a record only. It changes no setting and no trading state, and
// nothing in Policy, Risk, order placement, execution, the Jev prompts or the
// Feature Engine reads it (isolation_test.go pins that). Writes are appended:
// a create writes version 1 and an update writes the next version, so every
// earlier version stays readable; no statement here UPDATEs or DELETEs a
// version row (the table refuses both by trigger as well). Writing is mcpset's
// job (the MCP writer, behind the write switch); reading serves the MCP tools
// and the Settings screen's read-only list.
package researchnote

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/mask"
)

// Conclusions of a note: supported (支持), unsupported (不支持), pending (保留).
const (
	ConclusionSupported   = "supported"
	ConclusionUnsupported = "unsupported"
	ConclusionPending     = "pending"
)

// Kinds of an evidence link.
const (
	KindExperimentID     = "experiment_id"
	KindBacktestResultID = "backtest_result_id"
	KindComparisonLedger = "comparison_ledger"
	KindURL              = "url"
	KindOther            = "other"
)

// Actor of a version row: the MCP is the only writer.
const ActorMCP = "mcp"

// Limits (the CHECKs of migration 000057). Text limits count characters.
const (
	MaxQuestionRunes      = 500
	MaxHypothesisRunes    = 1000
	MaxActionsRunes       = 3000
	MaxResultRunes        = 3000
	MaxEvidenceLinks      = 20
	MaxEvidenceRefRunes   = 300
	MaxTags               = 10
	MaxTagRunes           = 40
	MaxQueryRunes         = 200
	MaxVersionsPerNote    = 50
	listExcerptRunes      = 200
	maxEvidenceJSONLength = 20000
)

var (
	// ErrNotFound is returned for an unknown note id.
	ErrNotFound = errors.New("researchnote: not found")
	// ErrVersionConflict is returned by an update whose expected_version is not
	// the note's current version. Nothing was stored.
	ErrVersionConflict = errors.New("researchnote: version conflict")
)

// ConflictError carries the version the note is at. It matches ErrVersionConflict.
type ConflictError struct{ Current int }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("researchnote: version conflict (current version %d)", e.Current)
}

// Is makes errors.Is(err, ErrVersionConflict) true.
func (e *ConflictError) Is(target error) bool { return target == ErrVersionConflict }

// InvalidError is a validation failure. Reason names the argument and the rule
// and never quotes a value, so it is safe to show to the caller.
type InvalidError struct{ Reason string }

func (e *InvalidError) Error() string { return "researchnote: " + e.Reason }

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

// Evidence is one link to what backs a note. Ref is an id, a ledger condition
// or a URL, by Kind.
type Evidence struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

// Fields is the content of one version of a note.
type Fields struct {
	Question      string     `json:"question"`
	Hypothesis    string     `json:"hypothesis"`
	Actions       string     `json:"actions"`
	ResultSummary string     `json:"result_summary"`
	Conclusion    string     `json:"conclusion"`
	EvidenceLinks []Evidence `json:"evidence_links"`
	Tags          []string   `json:"tags"`
}

// Patch is an update: a nil field keeps the current value, a non-nil one
// replaces it (an empty evidence / tag list clears it).
type Patch struct {
	Question      *string
	Hypothesis    *string
	Actions       *string
	ResultSummary *string
	Conclusion    *string
	EvidenceLinks *[]Evidence
	Tags          *[]string
}

func (p Patch) empty() bool {
	return p.Question == nil && p.Hypothesis == nil && p.Actions == nil && p.ResultSummary == nil &&
		p.Conclusion == nil && p.EvidenceLinks == nil && p.Tags == nil
}

// apply returns base with the patch's fields replaced.
func (p Patch) apply(base Fields) Fields {
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	set(&base.Question, p.Question)
	set(&base.Hypothesis, p.Hypothesis)
	set(&base.Actions, p.Actions)
	set(&base.ResultSummary, p.ResultSummary)
	set(&base.Conclusion, p.Conclusion)
	if p.EvidenceLinks != nil {
		base.EvidenceLinks = *p.EvidenceLinks
	}
	if p.Tags != nil {
		base.Tags = *p.Tags
	}
	return base
}

// Clean validates f and returns the form that is stored: text trimmed and
// key-shaped strings masked (mask.Text), conclusion defaulted to pending, tags
// lower-cased and de-duplicated. Lengths are checked before masking (it bounds
// the work of the masking) and after it (a mask can be longer than what it
// replaced), so a stored value always fits its CHECK. The reasons never quote a value.
func (f Fields) Clean() (Fields, error) {
	var err error
	if f.Question, err = cleanText(f.Question, "question", MaxQuestionRunes, true); err != nil {
		return Fields{}, err
	}
	if f.Hypothesis, err = cleanText(f.Hypothesis, "hypothesis", MaxHypothesisRunes, false); err != nil {
		return Fields{}, err
	}
	if f.Actions, err = cleanText(f.Actions, "actions", MaxActionsRunes, false); err != nil {
		return Fields{}, err
	}
	if f.ResultSummary, err = cleanText(f.ResultSummary, "result_summary", MaxResultRunes, false); err != nil {
		return Fields{}, err
	}
	switch f.Conclusion {
	case "":
		f.Conclusion = ConclusionPending
	case ConclusionSupported, ConclusionUnsupported, ConclusionPending:
	default:
		return Fields{}, invalid("conclusion は %s / %s / %s のどれかです", ConclusionSupported, ConclusionUnsupported, ConclusionPending)
	}
	if f.EvidenceLinks, err = cleanEvidence(f.EvidenceLinks); err != nil {
		return Fields{}, err
	}
	if f.Tags, err = cleanTags(f.Tags); err != nil {
		return Fields{}, err
	}
	return f, nil
}

func cleanEvidence(in []Evidence) ([]Evidence, error) {
	if len(in) > MaxEvidenceLinks {
		return nil, invalid("evidence_links は %d 件以内です", MaxEvidenceLinks)
	}
	out := make([]Evidence, 0, len(in))
	for _, e := range in {
		switch e.Kind {
		case KindExperimentID, KindBacktestResultID, KindComparisonLedger, KindURL, KindOther:
		default:
			return nil, invalid("evidence_links の kind は %s / %s / %s / %s / %s のどれかです",
				KindExperimentID, KindBacktestResultID, KindComparisonLedger, KindURL, KindOther)
		}
		ref, err := cleanText(e.Ref, "evidence_links の ref", MaxEvidenceRefRunes, true)
		if err != nil {
			return nil, err
		}
		out = append(out, Evidence{Kind: e.Kind, Ref: ref})
	}
	return out, nil
}

func cleanTags(in []string) ([]string, error) {
	if len(in) > MaxTags {
		return nil, invalid("tags は %d 件以内です", MaxTags)
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, raw := range in {
		tag, err := cleanText(raw, "tags の各タグ", MaxTagRunes, true)
		if err != nil {
			return nil, err
		}
		// Lower-case after masking: the masking rules are case-sensitive for some prefixes.
		if tag = strings.ToLower(tag); !seen[tag] {
			seen[tag] = true
			out = append(out, tag)
		}
	}
	return out, nil
}

// cleanText trims s, checks it is at most max characters before and after
// masking, and free of control characters (a newline and a tab are allowed in
// free text). A required field must not be empty.
func cleanText(s, field string, max int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	switch {
	case required && s == "":
		return "", invalid("%s が空です", field)
	case utf8.RuneCountInString(s) > max:
		return "", invalid("%s は %d 文字以内です", field, max)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return "", invalid("%s に制御文字は使えません", field)
		}
	}
	s = mask.Text(s)
	if utf8.RuneCountInString(s) > max {
		return "", invalid("%s は伏せ字（%s）に置き換えたあとも %d 文字以内です", field, mask.Replacement, max)
	}
	return s, nil
}
