package jevtrialsettings_test

import (
	"errors"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

const armWithCutoff = `[{"id":"a","provider":"openrouter","model":"vendor/m","prompt_version":"trader-v3","llm_cutoff":"2026-10-01"}]`

// An arm without llm_cutoff is valid and its cutoff is unknown (backwards compatible).
func TestArm_LLMCutoffIsOptional(t *testing.T) {
	arms, err := jevtrialsettings.ParseArms(`[{"id":"a","provider":"openrouter","model":"vendor/m","prompt_version":"trader-v3"}]`)
	if err != nil {
		t.Fatalf("ParseArms: %v", err)
	}
	if _, known := arms[0].TargetableFrom(); known {
		t.Error("an arm without llm_cutoff has a known cutoff: no date may be invented")
	}
	enc, err := jevtrialsettings.EncodeArms(arms)
	if err != nil || enc != `[{"id":"a","provider":"openrouter","model":"vendor/m","prompt_version":"trader-v3"}]` {
		t.Errorf("EncodeArms = %q, %v: an unset cutoff must not be written", enc, err)
	}
}

// A published cutoff makes only the days AFTER it targetable.
func TestArm_TargetableFromIsTheDayAfterTheCutoff(t *testing.T) {
	arms, err := jevtrialsettings.ParseArms(armWithCutoff)
	if err != nil {
		t.Fatalf("ParseArms: %v", err)
	}
	from, known := arms[0].TargetableFrom()
	if want := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC); !known || !from.Equal(want) {
		t.Errorf("TargetableFrom = %v, %v, want %v, true", from, known, want)
	}
}

func TestArm_BadLLMCutoffIsRejectedAsACutoffError(t *testing.T) {
	_, err := jevtrialsettings.ParseArms(`[{"id":"a","provider":"openrouter","model":"vendor/m","prompt_version":"trader-v3","llm_cutoff":"soon"}]`)
	var ae *jevtrialsettings.ArmsError
	if !errors.As(err, &ae) || ae.Kind != jevtrialsettings.ArmsErrLLMCutoff {
		t.Errorf("ParseArms(bad cutoff) = %v, want an ArmsErrLLMCutoff error", err)
	}
}
