package evalflow_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

// The shadow's answers are never an input of trading (issue #872): nothing that
// decides, sizes, places or learns from a trade may even name the shadow's
// table, repository, settings or runner. This pins that structurally: the
// packages below are the consumers of jev_decisions and of the trading flow, and
// a reference to any shadow symbol in them fails the build.
func TestShadowIsNotReferencedByTradingRAGCalibrationOrTraining(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..") // repository root
	consumers := []string{
		"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
		"internal/service/metalabel", "internal/repository/metalabel",
		"internal/service/rag", "internal/service/featureengine",
		"internal/service/calibration", "internal/repository/calibration",
		"internal/service/selfimprove", "internal/service/backtest", "internal/service/broker",
		"internal/repository/trading", "internal/repository/decisiontrade", "internal/repository/decisionsnapshot",
	}
	shadowSymbols := []string{
		"jev_shadow_decisions", "ShadowRepository", "ShadowSubmitter", "ShadowTrader", "jevshadowsettings",
		"service/jev/shadow", "domain/jevshadow", "service/jevprompt",
	}
	for _, dir := range consumers {
		abs := filepath.Join(root, dir)
		if _, err := os.Stat(abs); err != nil {
			t.Fatalf("consumer package %s is gone or moved: update this guard (%v)", dir, err)
		}
		err := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			src, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			for _, sym := range shadowSymbols {
				if strings.Contains(string(src), sym) {
					t.Errorf("%s references the Jev shadow (%q): shadow answers must never reach trading, RAG, Calibration or training", path, sym)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The jev_decisions readers that feed RAG, Calibration and the meta-model read
// jev_decisions only: their SQL never joins the shadow table (the shadow table's
// name appears in no repository other than the shadow's own).
func TestOnlyTheShadowRepositoryTouchesTheShadowTable(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "internal", "repository")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if strings.Contains(string(src), "jev_shadow_decisions") && filepath.Base(path) != "shadow_repo.go" {
			t.Errorf("%s mentions jev_shadow_decisions: only judgement/shadow_repo.go may", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Sol/Opus (the self-improvement loop) can change policy.* thresholds only. The
// prompt version, the shadow switch, the candidate and the cost cap are not among
// them and are not Settings operational keys (issue #872). The MCP's settings_set
// holds exactly the switch and the candidate (issue #929), nothing else of these.
func TestPromptAndShadowKeysAreOutsideEveryAutomaticWritePath(t *testing.T) {
	mcpWritable := map[string]bool{jevshadowsettings.KeyEnabled: true, jevshadowsettings.KeyCandidateVersion: true}
	for _, key := range jevshadowsettings.Keys() {
		err := domain.ValidatePolicyChanges([]domain.PolicyChange{{Key: key, OldValue: `"trader-v3"`, NewValue: `"trader-v4"`}})
		if err == nil {
			t.Errorf("ValidatePolicyChanges accepts %q: the self-improvement loop could change it", key)
		}
		if got := mcpwritesettings.IsAllowed(key); got != mcpWritable[key] {
			t.Errorf("%q: on the MCP settings_set whitelist = %v, want %v", key, got, mcpWritable[key])
		}
		if opsettings.IsKnownKey(key) {
			t.Errorf("%q is a generic Settings key", key)
		}
	}
}
