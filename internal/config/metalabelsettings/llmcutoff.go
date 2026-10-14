package metalabelsettings

import (
	"fmt"
	"time"
)

// LLMCutoffLayout is the date format of KeyLLMCutoff (start of that UTC day).
const LLMCutoffLayout = "2006-01-02"

// LLMCutoffTime returns LLMCutoff as the start of that UTC day.
func (s Settings) LLMCutoffTime() (time.Time, error) {
	t, err := time.Parse(LLMCutoffLayout, s.LLMCutoff)
	if err != nil {
		return time.Time{}, fmt.Errorf("metalabelsettings: %s %q is not a YYYY-MM-DD date: %w", KeyLLMCutoff, s.LLMCutoff, err)
	}
	return t, nil
}

func normalizeLLMCutoff(raw string) (string, error) {
	t, err := time.Parse(LLMCutoffLayout, raw)
	if err != nil {
		return "", fmt.Errorf("%s は YYYY-MM-DD 形式の日付で指定してください（Jev のモデルの学習データがいつまでかを入れます）", KeyLLMCutoff)
	}
	return t.Format(LLMCutoffLayout), nil
}
