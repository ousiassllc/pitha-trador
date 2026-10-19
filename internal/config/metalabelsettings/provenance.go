package metalabelsettings

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Provenance runtime_settings keys (issue #776): where KeyLLMCutoff comes from.
// All are JSON strings; none has a default (empty = not recorded).
const (
	// KeyLLMCutoffModel is the Jev model name the cutoff was confirmed for. The
	// nightly retraining compares it with jev_decisions.model_id: once Jev
	// answers with another model the cutoff no longer describes it and the OOS
	// evaluation is not used for adoption until the cutoff is re-confirmed.
	KeyLLMCutoffModel = "metalabel.llm_cutoff_model"
	// KeyLLMCutoffConfirmedOn is the date ("YYYY-MM-DD") the operator confirmed it.
	KeyLLMCutoffConfirmedOn = "metalabel.llm_cutoff_confirmed_on"
	// KeyLLMCutoffSource is the evidence (e.g. the vendor's official page).
	KeyLLMCutoffSource = "metalabel.llm_cutoff_source"
)

// MaxProvenanceLen is the longest model name / source text accepted (characters).
const MaxProvenanceLen = 200

// Mode is the operating mode a model adoption is judged for.
type Mode string

const (
	// ModePaper is the default: an unverified cutoff only warns.
	ModePaper Mode = "paper"
	// ModeLive is the pre-Live check: an unverified cutoff stops the adoption.
	ModeLive Mode = "live"
)

// Cutoff statuses (CutoffStatus).
const (
	// CutoffPlaceholder: LLMCutoff is unset or the shipped placeholder
	// (DefaultLLMCutoff); the OOS look-ahead protection does nothing, so the
	// nightly retraining never adopts a model.
	CutoffPlaceholder = "placeholder"
	// CutoffUnverified: a real cutoff, but the Jev model it was confirmed for
	// is not recorded, so a Jev model switch cannot be detected. A warning in
	// Paper; the adoption is stopped before Live (ModeLive).
	CutoffUnverified = "unverified"
	// CutoffConfirmed: a real cutoff with the Jev model recorded.
	CutoffConfirmed = "confirmed"
)

// CutoffStatus classifies the LLM cutoff setting.
func (s Settings) CutoffStatus() string {
	switch {
	case s.LLMCutoff == DefaultLLMCutoff:
		return CutoffPlaceholder
	case s.LLMCutoffModel == "":
		return CutoffUnverified
	}
	return CutoffConfirmed
}

// CutoffWarning is the operator-facing warning for the Settings and
// Calibration screens, or "" when the cutoff is confirmed.
func (s Settings) CutoffWarning() string {
	switch s.CutoffStatus() {
	case CutoffPlaceholder:
		return "Jev モデルの学習データの終わりが仮置き（" + DefaultLLMCutoff + "）のままです。評価期間が LLM の学習期間と重なり OOS が過大評価され得るため、" +
			"夜間再学習のモデルは採用されず「不採用」として記録されます。実際の日付と根拠を設定してください。"
	case CutoffUnverified:
		return "学習データの終わりの根拠（Jev のモデル名・確認日・出典）が未記録です。Jev のモデルが切り替わっても検知できません。" +
			"Paper では警告のまま採用されますが、Live 前チェックでは採用を止めます。確認した Jev のモデル名を " + KeyLLMCutoffModel + " に記録してください。"
	}
	return ""
}

// CutoffInfo is the Calibration screen's view of the cutoff setting, shown even
// before any model was trained (issue #776).
type CutoffInfo struct {
	// Cutoff is KeyLLMCutoff ("YYYY-MM-DD", UTC).
	Cutoff string
	// Status is one of CutoffPlaceholder, CutoffUnverified, CutoffConfirmed.
	Status string
	// Warning is the operator-facing warning; empty when Status is CutoffConfirmed.
	Warning string
	// Model, ConfirmedOn and Source are the recorded provenance (empty when not recorded).
	Model, ConfirmedOn, Source string
}

// CutoffInfo returns the cutoff setting with its status and provenance.
func (s Settings) CutoffInfo() CutoffInfo {
	return CutoffInfo{
		Cutoff: s.LLMCutoff, Status: s.CutoffStatus(), Warning: s.CutoffWarning(),
		Model: s.LLMCutoffModel, ConfirmedOn: s.LLMCutoffConfirmedOn, Source: s.LLMCutoffSource,
	}
}

func (s Settings) provenanceText(key string) (string, bool) {
	switch key {
	case KeyLLMCutoffModel:
		return s.LLMCutoffModel, true
	case KeyLLMCutoffConfirmedOn:
		return s.LLMCutoffConfirmedOn, true
	case KeyLLMCutoffSource:
		return s.LLMCutoffSource, true
	}
	return "", false
}

func (s *Settings) setProvenance(key, v string) {
	switch key {
	case KeyLLMCutoffModel:
		s.LLMCutoffModel = v
	case KeyLLMCutoffConfirmedOn:
		s.LLMCutoffConfirmedOn = v
	case KeyLLMCutoffSource:
		s.LLMCutoffSource = v
	}
}

// normalizeProvenance validates a provenance value: a date for the
// confirmation day, otherwise non-empty text of at most MaxProvenanceLen
// characters without control characters.
func normalizeProvenance(key, raw string) (string, error) {
	if key == KeyLLMCutoffConfirmedOn {
		t, err := time.Parse(LLMCutoffLayout, raw)
		if err != nil {
			return "", fmt.Errorf("%s は YYYY-MM-DD 形式の日付で指定してください", key)
		}
		return t.Format(LLMCutoffLayout), nil
	}
	if raw == "" {
		return "", fmt.Errorf("%s は空にできません", key)
	}
	if utf8.RuneCountInString(raw) > MaxProvenanceLen {
		return "", fmt.Errorf("%s は %d 文字以内で指定してください", key, MaxProvenanceLen)
	}
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%s に制御文字（改行・タブなど）は使えません", key)
	}
	return raw, nil
}

// validateProvenance checks the recorded provenance of a whole Settings value;
// an empty value is "not recorded" and valid.
func (s Settings) validateProvenance() []error {
	var errs []error
	for _, key := range []string{KeyLLMCutoffModel, KeyLLMCutoffConfirmedOn, KeyLLMCutoffSource} {
		v, _ := s.provenanceText(key)
		if v == "" {
			continue
		}
		if canonical, err := normalizeProvenance(key, v); err != nil {
			errs = append(errs, err)
		} else if canonical != v {
			errs = append(errs, fmt.Errorf("%s は正規化した形式で指定してください", key))
		}
	}
	return errs
}
