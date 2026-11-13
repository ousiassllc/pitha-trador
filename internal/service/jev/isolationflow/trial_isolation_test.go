package isolationflow_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

const modulePath = "github.com/ousiassllc/pitha-trador/"

// trialRunnerDirs are the packages that call the trial's models: the runner and
// the wire client. trialflow (tests only) may be absent.
var trialRunnerDirs = []struct {
	dir      string
	optional bool
}{
	{"internal/service/jev/trial", false},
	{"internal/service/jev/trialwire", false},
	{"internal/service/jev/trialflow", true},
}

// The trial's answers are never an input of trading (issue #1059): nothing that
// decides, sizes, places or learns from a trade may even name the trial's table,
// repository, settings or runner. Same guard as the shadow's
// (evalflow.TestShadowIsNotReferencedByTradingRAGCalibrationOrTraining).
func TestTrialIsNotReferencedByTradingRAGCalibrationOrTraining(t *testing.T) {
	consumers := []string{
		"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
		"internal/service/metalabel", "internal/repository/metalabel",
		"internal/service/rag", "internal/service/featureengine",
		"internal/service/calibration", "internal/repository/calibration",
		"internal/service/selfimprove", "internal/service/backtest", "internal/service/broker",
		"internal/repository/trading", "internal/repository/decisiontrade", "internal/repository/decisionsnapshot",
	}
	trialSymbols := []string{
		"jev_trial_decisions", "TrialRepository", "jevtrialsettings", "service/jev/trial", "domain/jevtrial", "jevtrial", "trialwire",
	}
	for _, dir := range consumers {
		walkGoSources(t, dir, false, func(path string, src []byte) {
			for _, sym := range trialSymbols {
				if strings.Contains(string(src), sym) {
					t.Errorf("%s references the Jev trial (%q): trial answers must never reach trading, RAG, Calibration or training", path, sym)
				}
			}
		})
	}
}

// Only the trial repository's file names the trial table: the jev_decisions
// readers (RAG, Calibration, meta-model) and every other repository never see it.
func TestOnlyTheTrialRepositoryTouchesTheTrialTable(t *testing.T) {
	walkGoSources(t, "internal/repository", false, func(path string, src []byte) {
		if strings.Contains(string(src), "jev_trial_decisions") && !strings.HasSuffix(path, "judgement/trial_repo.go") {
			t.Errorf("%s mentions jev_trial_decisions: only judgement/trial_repo.go may", path)
		}
	})
}

// No trial key can be changed by the self-improvement loop, the MCP or the
// generic operational Settings path: jev.trial.enabled in particular is turned on
// from the app's Settings screen only (issue #1059). The trial and shadow key
// spaces never overlap.
func TestTrialKeysAreOutsideEveryAutomaticWritePath(t *testing.T) {
	keys := append(jevtrialsettings.Keys(),
		jevtrialsettings.HaltKey("a"), jevtrialsettings.FailuresKey("a"), jevtrialsettings.ThrottleKey("a"))
	for _, key := range keys {
		err := domain.ValidatePolicyChanges([]domain.PolicyChange{{Key: key, OldValue: `0`, NewValue: `1`}})
		if err == nil {
			t.Errorf("ValidatePolicyChanges accepts %q: the self-improvement loop could change it", key)
		}
		if mcpwritesettings.IsAllowed(key) {
			t.Errorf("%q is on the MCP settings_set whitelist: the trial must not be writable by the MCP", key)
		}
		if opsettings.IsKnownKey(key) {
			t.Errorf("%q is a generic Settings key", key)
		}
		if strings.HasPrefix(key, "jev.shadow.") {
			t.Errorf("trial key %q lives in the shadow's key space", key)
		}
	}
	for _, key := range jevshadowsettings.Keys() {
		if strings.HasPrefix(key, "jev.trial.") {
			t.Errorf("shadow key %q lives in the trial's key space", key)
		}
	}
}

// The trial runner and its wire client import none of the trading or learning
// packages (so a trial answer has no path into them), and read none of the live
// Jev's secrets: they use OPENROUTER_* only (issue #1059). service/rag is allowed:
// the runner receives the RAG context the live call already built.
func TestTrialRunnerImportsNoTradingPackageAndReadsNoLiveJevSecret(t *testing.T) {
	banned := []string{
		"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
		"internal/service/metalabel", "internal/service/calibration", "internal/service/featureengine",
		"internal/service/selfimprove", "internal/service/backtest", "internal/service/broker",
		"internal/repository/metalabel", "internal/repository/calibration", "internal/repository/trading",
		"internal/repository/decisiontrade", "internal/repository/decisionsnapshot",
	}
	liveJevSecrets := []string{"JEV_API_KEY", "JEV_BASE_URL", "JEV_MODEL", "config.KeyJev", "KeyJevAPIKey", "JevAPIKey", "JevBaseURL", "JevModel"}
	for _, d := range trialRunnerDirs {
		walkGoSources(t, d.dir, d.optional, func(path string, src []byte) {
			for _, imp := range importsOf(t, path) {
				for _, b := range banned {
					if imp == modulePath+b || strings.HasPrefix(imp, modulePath+b+"/") {
						t.Errorf("%s imports %s: the trial runner must not depend on trading/learning packages", path, imp)
					}
				}
			}
			for _, name := range liveJevSecrets {
				if strings.Contains(string(src), name) {
					t.Errorf("%s mentions %q: a trial arm must not read the live Jev's secrets", path, name)
				}
			}
		})
	}
}

// OPENROUTER_* are optional secrets: the app starts and the Setup Guard stays
// quiet without them (issue #1059).
func TestOpenRouterSecretsAreNotRequired(t *testing.T) {
	for _, key := range config.RequiredSecretKeys() {
		if strings.HasPrefix(key, "OPENROUTER_") {
			t.Errorf("%s is a required secret", key)
		}
	}
	for _, key := range config.RequiredSetup(config.BrokerSettings{}).SecretKeys {
		if strings.HasPrefix(key, "OPENROUTER_") {
			t.Errorf("%s is required by the Setup Guard", key)
		}
	}
	for _, key := range []string{config.KeyOpenRouterAPIKey, config.KeyOpenRouterBaseURL} {
		if !config.IsAllowedSecretKey(key) {
			t.Errorf("%s is not an allowed (optional) secret key", key)
		}
	}
}
