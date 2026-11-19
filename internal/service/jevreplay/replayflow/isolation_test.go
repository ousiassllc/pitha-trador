package replayflow_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevreplaysettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

const (
	repoRoot   = "../../../.."
	modulePath = "github.com/ousiassllc/pitha-trador/"
)

// goSources returns the non-test Go files under dir (relative to the repository root).
func goSources(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return err
	})
	if err != nil {
		t.Fatalf("%s: %v", dir, err)
	}
	return out
}

func importsOf(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var out []string
	for _, spec := range file.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// The replay switch is outside every automatic write path: the MCP's
// settings_set whitelist, the generic Settings keys, the self-improvement loop,
// and the shadow's and the trial's key spaces (issue #1085).
func TestReplayKeyIsOutsideEveryAutomaticWritePath(t *testing.T) {
	for _, key := range jevreplaysettings.Keys() {
		if err := domain.ValidatePolicyChanges([]domain.PolicyChange{{Key: key, OldValue: `0`, NewValue: `1`}}); err == nil {
			t.Errorf("ValidatePolicyChanges accepts %q: the self-improvement loop could change it", key)
		}
		if mcpwritesettings.IsAllowed(key) {
			t.Errorf("%q is on the MCP settings_set whitelist: the MCP must not turn the replay on", key)
		}
		if opsettings.IsKnownKey(key) {
			t.Errorf("%q is a generic Settings key", key)
		}
		if strings.HasPrefix(key, jevtrialsettings.KeyPrefix) || strings.HasPrefix(key, "jev.shadow.") {
			t.Errorf("replay key %q lives in the trial's or the shadow's key space", key)
		}
	}
	for _, key := range append(jevtrialsettings.Keys(), jevshadowsettings.Keys()...) {
		if jevreplaysettings.IsKey(key) {
			t.Errorf("%q is read as a replay key", key)
		}
	}
	if jevreplaysettings.DefaultEnabled {
		t.Error("the replay is on by default")
	}
}

// Nothing that decides, sizes, places or learns from a trade imports a replay
// package or names its tables, and the replay's own packages import none of the
// trading or learning packages (so a replay result has no path into them).
func TestReplayHasNoPathToTradingRAGCalibrationOrTraining(t *testing.T) {
	banned := []string{
		"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
		"internal/service/metalabel", "internal/service/calibration", "internal/service/featureengine",
		"internal/service/selfimprove", "internal/service/backtest", "internal/service/broker",
		"internal/repository/metalabel", "internal/repository/calibration", "internal/repository/trading",
		"internal/repository/decisiontrade", "internal/repository/decisionsnapshot", "internal/service/rag",
	}
	replayDirs := []string{
		"internal/service/jevreplay", "internal/repository/jevreplay", "internal/domain/jevreplay",
		"internal/config/jevreplaysettings", "internal/service/paperresultsmcp/replaytool", "internal/bootstrap/replaywire",
	}
	for _, dir := range replayDirs {
		for _, path := range goSources(t, dir) {
			for _, imp := range importsOf(t, path) {
				for _, b := range banned {
					if imp == modulePath+b || strings.HasPrefix(imp, modulePath+b+"/") {
						t.Errorf("%s imports %s: a replay package must not depend on trading/learning packages", path, imp)
					}
				}
			}
		}
	}
	consumers := []string{
		"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
		"internal/service/metalabel", "internal/repository/metalabel", "internal/service/rag", "internal/service/featureengine",
		"internal/service/calibration", "internal/repository/calibration", "internal/service/selfimprove", "internal/service/backtest",
		"internal/service/broker", "internal/repository/trading", "internal/repository/decisiontrade", "internal/repository/decisionsnapshot",
		"internal/repository/judgement", "internal/service/jev/shadow",
	}
	symbols := []string{"jev_replay_", "service/jevreplay", "repository/jevreplay", "domain/jevreplay", "jevreplaysettings"}
	for _, dir := range consumers {
		for _, path := range goSources(t, dir) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, sym := range symbols {
				if strings.Contains(string(src), sym) {
					t.Errorf("%s references the replay (%q): replay results must never reach trading, RAG, Calibration, the shadow or training", path, sym)
				}
			}
		}
	}
}

// Only the replay repository writes the replay tables, and no code outside the
// replay packages and the read-only MCP query names them.
func TestOnlyTheReplayPackagesTouchTheReplayTables(t *testing.T) {
	allowed := []string{
		"internal/repository/jevreplay/", "internal/domain/jevreplay/", "internal/service/jevreplay/", "internal/service/paperresultsmcp/replaytool/",
		"internal/service/paperresultsmcp/query/trialquery/", "internal/service/paperresultsmcp/mcpset/replay.go",
		"internal/service/paperresultsmcp/replay_tools.go",
	}
	for _, path := range goSources(t, "internal") {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "jev_replay_") {
			continue
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, repoRoot+"/"))
		ok := false
		for _, a := range allowed {
			ok = ok || strings.HasPrefix(rel, a)
		}
		if !ok {
			t.Errorf("%s mentions a replay table: only the replay repository and the MCP's read queries may", rel)
		}
	}
}
