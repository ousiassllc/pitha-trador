package trainingflow_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel/training"
)

// The defaults are derived, not picked: the minimum take count is the power
// analysis of the assumed edge and dispersion, and every other default follows
// from it (docs FR-META-7).
func TestDefaultAcceptanceConfig_FollowsThePowerAnalysis(t *testing.T) {
	cfg := training.DefaultAcceptanceConfig()
	deff := training.DesignEffect(training.AssumedRowsPerDay*training.AssumedTakeRate, training.AssumedWinProbDayShiftSD)
	want := training.RequiredTakes(training.AssumedSigmaBps, training.AssumedEdgeBps, training.DefaultAlpha, training.DesignPower, deff)
	if cfg.MinTakes != want {
		t.Errorf("MinTakes = %d, want RequiredTakes = %d", cfg.MinTakes, want)
	}
	if float64(cfg.MinOOSSamples)*training.AssumedTakeRate < float64(cfg.MinTakes) {
		t.Errorf("MinOOSSamples %d at a %.0f%% take rate cannot yield MinTakes %d", cfg.MinOOSSamples, 100*training.AssumedTakeRate, cfg.MinTakes)
	}
	// Spot-check the formula with a case worked by hand: without clustering,
	// ((1.645 + 0.842) * 30 / 5)^2 = 222.7 for a 5% one-sided test at 80% power.
	if got := training.RequiredTakes(30, 5, 0.05, 0.8, 1); got < 222 || got > 224 {
		t.Errorf("RequiredTakes(30, 5, 5%%, 80%%, 1) = %d, want about 223", got)
	}
	if cfg.MinAUCLower != 0.5 || cfg.MinEVLowerBps != 0 || cfg.MinOOSDays != training.DefaultMinOOSDays || cfg.Alpha != 0.01 || cfg.Resamples != 2000 {
		t.Errorf("defaults = %+v", cfg)
	}
}

// The spec states the defaults in numbers; they must match the code.
func TestDefaultAcceptanceConfig_MatchesTheSpec(t *testing.T) {
	raw, err := os.ReadFile("../../../../../docs/requirements/functional/components-pipeline.md")
	if err != nil {
		t.Fatalf("read the spec: %v", err)
	}
	// The FR-META-7 requirement: its line and the indented sub-bullets under it.
	var fr string
	inFR := false
	for _, line := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(line, "- FR-META-7"):
			inFR = true
			fr += line + "\n"
		case inFR && strings.HasPrefix(line, "  "):
			fr += line + "\n"
		default:
			inFR = false
		}
	}
	if fr == "" {
		t.Fatal("FR-META-7 is missing from the spec")
	}
	cfg := training.DefaultAcceptanceConfig()
	for name, v := range map[string]int{"最小take件数": cfg.MinTakes, "最小OOS件数": cfg.MinOOSSamples, "最小OOS営業日数": cfg.MinOOSDays, "ブートストラップ回数": cfg.Resamples} {
		if !strings.Contains(fr, strconv.Itoa(v)) {
			t.Errorf("FR-META-7 does not state the default %s %d", name, v)
		}
	}
	limits := changeguard.DefaultLimits()
	for _, want := range []string{
		"前回の採用から**" + strconv.Itoa(limits.ModelSwapDays) + "営業日**",
		"から**" + strconv.Itoa(limits.FreezeDays) + "営業日**はモデルを採用しない",
	} {
		if !strings.Contains(fr, want) {
			t.Errorf("FR-META-7 does not state the change limit %q", want)
		}
	}
	walk := training.DefaultWalkForwardConfig()
	for _, want := range []string{
		"σ=30", "11 bps", "検出力80%", "デザイン効果", "案",
		"窓の上限（" + strconv.Itoa(int(walk.OOSMaxWindow.Hours()/24)) + "日", // the OOS window's reach-back limit
		"fit" + strconv.Itoa(walk.MinFit),                               // the training side's minimum
	} {
		if !strings.Contains(fr, want) {
			t.Errorf("FR-META-7 does not state the assumption %q", want)
		}
	}
}
