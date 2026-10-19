package metaflow_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/paperbootstrap"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

// switchSource is the Settings switch of the Paper-only bootstrap entry
// (issue #798); reads counts how often a decision looked at it.
type switchSource struct {
	on    bool
	err   error
	reads int
}

func (s *switchSource) PaperBootstrapEnabled(context.Context) (bool, error) {
	s.reads++
	return s.on, s.err
}

// countingRisk records the candidates that reach the Risk Engine's gate.
type countingRisk struct {
	fakeRiskChecker
	checks int
}

func (r *countingRisk) Check(ctx context.Context, id int64, direction string) (bool, string) {
	r.checks++
	return r.fakeRiskChecker.Check(ctx, id, direction)
}

type appliedProposal struct{ version string }

func (a appliedProposal) CurrentThresholds(context.Context) (config.PolicyConfig, error) {
	return testThresholds().Policy, nil
}
func (a appliedProposal) AppliedPolicyVersion(context.Context) (string, error) { return a.version, nil }

// bootstrapEngine is an Engine with no adopted model (the model source answers
// "none"), the bootstrap wired for mode and src, and rc as its Risk Engine.
func bootstrapEngine(env metaEnv, rc policy.RiskChecker, mode metalabelsettings.Mode, src paperbootstrap.Source, models policytest.Models, extra ...policy.Option) *policy.Engine {
	opts := append([]policy.Option{
		policytest.Option(models, &policytest.Predictions{}),
		policy.WithPaperBootstrap(mode, src),
	}, extra...)
	return policy.NewEngine(testThresholds(), rc, env.signals, opts...)
}

// Paper + the switch on + no adopted model: the candidate reaches the Risk
// Engine on the legacy rule and is marked as a bootstrap signal.
func TestBootstrap_PaperOnNoModelEntersOnTheLegacyRule(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	logs := captureLogs(t)
	risk := &countingRisk{fakeRiskChecker: fakeRiskChecker{passed: true}}
	src := &switchSource{on: true}

	sig, err := bootstrapEngine(env, risk, metalabelsettings.ModePaper, src, policytest.Models{}).Evaluate(context.Background(), env.in)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if sig.Direction != domain.JevDirectionLong || !sig.RiskPassed || sig.RejectReason != nil {
		t.Fatalf("signal = %+v (%s), want a LONG entry that passed the Risk Engine", sig, reasonOf(sig))
	}
	if risk.checks != 1 {
		t.Errorf("Risk Engine checked %d times, want 1 (the order proceeds to the Risk Engine)", risk.checks)
	}
	if sig.PolicyVersion != paperbootstrap.Version {
		t.Errorf("PolicyVersion = %q, want %q", sig.PolicyVersion, paperbootstrap.Version)
	}
	if sig.Score == nil || *sig.Score != 0.68 {
		t.Errorf("Score = %v, want Jev's confidence 0.68 (the legacy rule)", sig.Score)
	}
	stored, err := env.signals.Insert(context.Background(), sig)
	if err != nil || stored.PolicyVersion != paperbootstrap.Version {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	var marked bool
	for _, line := range decodeLogLines(t, logs) {
		if strings.HasPrefix(line["msg"].(string), "policy: paper bootstrap entry") {
			marked = true
			if line["policy_version"] != paperbootstrap.Version || line["direction"] != domain.JevDirectionLong || line["symbol"] != "7203" {
				t.Errorf("bootstrap log line = %+v", line)
			}
		}
	}
	if !marked {
		t.Errorf("no bootstrap log line in %s", logs.String())
	}
}

// The legacy rule's own gates still apply to a bootstrap entry: no new relaxed
// criterion, and the Risk Engine's refusal is final.
func TestBootstrap_KeepsTheLegacyGatesAndTheRiskEngine(t *testing.T) {
	for name, tc := range map[string]struct {
		edit       func(*policy.Input)
		risk       fakeRiskChecker
		wantReason string
		wantRisk   int
	}{
		"confidence below min_probability": {
			edit: func(in *policy.Input) { in.Decision.Confidence = ptr(0.50) }, risk: fakeRiskChecker{passed: true},
			wantReason: "probability_below_threshold",
		},
		"confidence bucket not calibrated": {
			edit: func(in *policy.Input) { in.Calibrated = false }, risk: fakeRiskChecker{passed: true},
			wantReason: policy.ReasonNotCalibrated,
		},
		"toxic flow guard": {
			edit: func(in *policy.Input) { in.Decision.ToxicFlow = ptr(0.90) }, risk: fakeRiskChecker{passed: true},
			wantReason: "toxic_flow_above_threshold",
		},
		"risk engine rejects": {
			edit: func(*policy.Input) {}, risk: fakeRiskChecker{passed: false, reason: "max_daily_trades"},
			wantReason: policy.ReasonRiskEngineRejected + ": max_daily_trades", wantRisk: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			env := newMetaEnv(t, domain.JevDirectionLong)
			tc.edit(&env.in)
			risk := &countingRisk{fakeRiskChecker: tc.risk}
			sig, err := bootstrapEngine(env, risk, metalabelsettings.ModePaper, &switchSource{on: true}, policytest.Models{}).Evaluate(context.Background(), env.in)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if sig.Direction != domain.JevDirectionNone || sig.RiskPassed || !strings.HasPrefix(reasonOf(sig), tc.wantReason) {
				t.Fatalf("signal = %+v (%s), want NONE with %q", sig, reasonOf(sig), tc.wantReason)
			}
			if strings.HasPrefix(reasonOf(sig), policy.ReasonMetaSkip) {
				t.Errorf("reason %q came from the meta rule, want the legacy rule's", reasonOf(sig))
			}
			if risk.checks != tc.wantRisk {
				t.Errorf("Risk Engine checked %d times, want %d", risk.checks, tc.wantRisk)
			}
			if sig.PolicyVersion != paperbootstrap.Version {
				t.Errorf("PolicyVersion = %q, want %q on every bootstrap decision", sig.PolicyVersion, paperbootstrap.Version)
			}
		})
	}
}

// HARD RULE: outside Paper the switch has no effect - without a model the
// candidate is still skipped before the Risk Engine - and is not even read.
func TestBootstrap_NeverBypassesTheModelOutsidePaper(t *testing.T) {
	for name, mode := range map[string]metalabelsettings.Mode{
		"Live":                   metalabelsettings.ModeLive,
		"a mode left unset":      "",
		"an unrecognised string": "paper-ish",
	} {
		t.Run(name, func(t *testing.T) {
			env := newMetaEnv(t, domain.JevDirectionLong)
			logs := captureLogs(t)
			risk := &countingRisk{fakeRiskChecker: fakeRiskChecker{passed: true}}
			src := &switchSource{on: true}

			sig, err := bootstrapEngine(env, risk, mode, src, policytest.Models{}).Evaluate(context.Background(), env.in)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			wantReason := policy.ReasonMetaSkip + ": " + metalabel.ReasonNoModel
			if sig.Direction != domain.JevDirectionNone || sig.RiskPassed || !strings.HasPrefix(reasonOf(sig), wantReason) {
				t.Fatalf("signal = %+v (%s), want NONE %q even with the switch on", sig, reasonOf(sig), wantReason)
			}
			if risk.checks != 0 {
				t.Errorf("Risk Engine checked %d times, want 0: the candidate must not reach it", risk.checks)
			}
			if src.reads != 0 {
				t.Errorf("the switch was read %d times, want 0 outside Paper", src.reads)
			}
			if sig.PolicyVersion != policy.MetaVersion {
				t.Errorf("PolicyVersion = %q, want %q", sig.PolicyVersion, policy.MetaVersion)
			}
			if strings.Contains(logs.String(), "paper bootstrap entry") {
				t.Errorf("a bootstrap log line was written outside Paper: %s", logs.String())
			}
		})
	}
}

// Without the option, or with the switch off or unreadable, nothing changes.
func TestBootstrap_OffOrUnreadableStaysOnTheModelRule(t *testing.T) {
	for name, tc := range map[string]struct {
		src  paperbootstrap.Source
		wire bool
	}{
		"switch off":         {src: &switchSource{on: false}, wire: true},
		"switch unreadable":  {src: &switchSource{on: true, err: errors.New("db down")}, wire: true},
		"option not applied": {wire: false},
	} {
		t.Run(name, func(t *testing.T) {
			env := newMetaEnv(t, domain.JevDirectionLong)
			risk := &countingRisk{fakeRiskChecker: fakeRiskChecker{passed: true}}
			opts := []policy.Option{policytest.Option(policytest.Models{}, &policytest.Predictions{})}
			if tc.wire {
				opts = append(opts, policy.WithPaperBootstrap(metalabelsettings.ModePaper, tc.src))
			}
			sig, err := policy.NewEngine(testThresholds(), risk, env.signals, opts...).Evaluate(context.Background(), env.in)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reasonOf(sig), policy.ReasonMetaSkip+": "+metalabel.ReasonNoModel) || risk.checks != 0 {
				t.Fatalf("signal = %+v (%s), risk checks %d, want the meta_skip NONE before the Risk Engine", sig, reasonOf(sig), risk.checks)
			}
		})
	}
}

// The bootstrap is for the time before a model exists: with an adopted model
// the model decides, and a model that cannot be loaded is no "no model".
func TestBootstrap_OnlyWhenNoModelIsAdopted(t *testing.T) {
	t.Run("adopted taking model", func(t *testing.T) {
		env := newMetaEnv(t, domain.JevDirectionLong)
		src := &switchSource{on: true}
		env.in.Decision.Confidence = ptr(0.40) // below the legacy bar: only the model can take it
		sig, err := bootstrapEngine(env, nil, metalabelsettings.ModePaper, src, policytest.ModelOf(policytest.TakingModel())).Evaluate(context.Background(), env.in)
		if err != nil || sig.Direction != domain.JevDirectionLong || sig.PolicyVersion != policy.MetaVersion {
			t.Fatalf("signal = %+v, %v, want the model's LONG under %s", sig, err, policy.MetaVersion)
		}
		if src.reads != 0 {
			t.Errorf("the switch was read %d times with an adopted model, want 0", src.reads)
		}
	})
	t.Run("adopted skipping model", func(t *testing.T) {
		env := newMetaEnv(t, domain.JevDirectionLong)
		env.in.Decision.Confidence = ptr(0.99) // far above the legacy bar: the model's skip is final
		sig, err := bootstrapEngine(env, nil, metalabelsettings.ModePaper, &switchSource{on: true}, policytest.ModelOf(policytest.SkippingModel())).Evaluate(context.Background(), env.in)
		if err != nil || sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reasonOf(sig), policy.ReasonMetaSkip+": "+metalabel.ReasonEVBelowMin) || sig.PolicyVersion != policy.MetaVersion {
			t.Fatalf("signal = %+v (%s), %v, want the model's meta_skip", sig, reasonOf(sig), err)
		}
	})
	t.Run("model cannot be loaded", func(t *testing.T) {
		env := newMetaEnv(t, domain.JevDirectionLong)
		risk := &countingRisk{fakeRiskChecker: fakeRiskChecker{passed: true}}
		src := &switchSource{on: true}
		sig, err := bootstrapEngine(env, risk, metalabelsettings.ModePaper, src, policytest.Models{Err: errors.New("feature layout changed")}).Evaluate(context.Background(), env.in)
		if err != nil || sig.Direction != domain.JevDirectionNone || !strings.HasPrefix(reasonOf(sig), policy.ReasonMetaModelUnavailable) || risk.checks != 0 {
			t.Fatalf("signal = %+v (%s), %v, want meta_model_unavailable before the Risk Engine", sig, reasonOf(sig), err)
		}
	})
}

// An applied Self-Improvement proposal's suffix is kept after the bootstrap
// marker, so both identify the signal.
func TestBootstrap_KeepsTheAppliedProposalSuffix(t *testing.T) {
	env := newMetaEnv(t, domain.JevDirectionLong)
	sig, err := bootstrapEngine(env, nil, metalabelsettings.ModePaper, &switchSource{on: true}, policytest.Models{},
		policy.WithPolicySource(appliedProposal{version: "sol-12"})).Evaluate(context.Background(), env.in)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if want := paperbootstrap.Version + "+sol-12"; sig.PolicyVersion != want {
		t.Errorf("PolicyVersion = %q, want %q", sig.PolicyVersion, want)
	}
	if len(sig.PolicyVersion) > 20 {
		t.Errorf("PolicyVersion %q is longer than the column's VARCHAR(20)", sig.PolicyVersion)
	}
}
