package isolationflow_test

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Import guard for the Jev model-comparison trial in both directions (issue
// #1069). TestTrialRunnerImportsNoTradingPackageAndReadsNoLiveJevSecret already
// pins the trial runner's direct imports; this file adds the reverse direction
// (nothing live imports the trial) and widens the forward one to every trial
// package. The graph is read from the module's non-test sources (production
// imports only; build tags are ignored on purpose, so a windows-only file counts).

// trialPackages are the packages of the trial: the Settings service, the runner,
// the wire client and the Settings screen's handler. trialflow holds tests only.
var trialPackages = []string{
	"internal/service/jevtrial", "internal/service/jev/trial", "internal/service/jev/trialwire",
	"internal/web/handler/jevtrial",
}

// trialImportTargets are the packages the live side must never import.
var trialImportTargets = []string{"internal/service/jevtrial", "internal/service/jev/trial", "internal/service/jev/trialwire"}

// allowedTrialImporters are the only packages that import a trial target, with
// the reason. Anything else (a new package that reaches the trial) must be
// added here on purpose; an entry that no longer imports one fails too.
var allowedTrialImporters = map[string]string{
	"internal/bootstrap":            "wires the runner and the Settings service into the app",
	"internal/web/handler/jevtrial": "the Settings screen's trial section (service/jevtrial)",
	"internal/web/handler/settings": "reads trialwire.DefaultBaseURL for the OPENROUTER_BASE_URL hint only",
	"internal/service/jev/trial":    "the runner builds its arms from trialwire",
}

// liveRoots are the live decision path, Risk, order placement, RAG, Calibration
// and the meta-model, with their sub-packages (the trial's own are excluded).
// Nothing under them may reach a trial package, directly or through another package.
var liveRoots = []string{
	"internal/service/jev", "internal/service/jevprompt", "internal/repository/judgement",
	"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
	"internal/service/broker", "internal/repository/trading", "internal/repository/decisiontrade",
	"internal/repository/decisionsnapshot", "internal/bootstrap/proposalexec", "internal/repository/proposalstore",
	"internal/service/rag", "internal/service/featureengine",
	"internal/service/calibration", "internal/repository/calibration",
	"internal/service/metalabel", "internal/repository/metalabel",
	"internal/service/selfimprove", "internal/service/backtest",
}

// trialForbiddenImports are the packages the trial must not import directly:
// the trading and learning packages the runner test bans, plus order placement
// and the MCP's order-proposal path. Not transitive: the wire client legitimately
// builds on the live Jev's prompt package, which itself sits above Risk.
var trialForbiddenImports = []string{
	"internal/service/policy", "internal/service/risk", "internal/service/execution", "internal/service/exitalign",
	"internal/service/metalabel", "internal/service/calibration", "internal/service/featureengine",
	"internal/service/selfimprove", "internal/service/backtest", "internal/service/broker",
	"internal/repository/metalabel", "internal/repository/calibration", "internal/repository/trading",
	"internal/repository/decisiontrade", "internal/repository/decisionsnapshot",
	"internal/bootstrap/proposalexec", "internal/repository/proposalstore", "internal/service/paperresultsmcp/proposal",
}

// importGraph maps a package directory (slash-separated, relative to the
// repository root) to the in-module package directories it imports.
type importGraph map[string][]string

func within(pkg, root string) bool { return pkg == root || strings.HasPrefix(pkg, root+"/") }

func withinAny(pkg string, roots []string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return within(pkg, r) })
}

// loadImportGraph reads every non-test Go source under internal/ and cmd/.
func loadImportGraph(t *testing.T) importGraph {
	t.Helper()
	g := importGraph{}
	for _, top := range []string{"internal", "cmd"} {
		walkGoSources(t, top, false, func(path string, _ []byte) {
			rel, err := filepath.Rel(repoRoot, filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			pkg := filepath.ToSlash(rel)
			for _, imp := range importsOf(t, path) {
				if dep, ok := strings.CutPrefix(imp, modulePath); ok && !slices.Contains(g[pkg], dep) {
					g[pkg] = append(g[pkg], dep)
				}
			}
			if _, ok := g[pkg]; !ok {
				g[pkg] = nil
			}
		})
	}
	return g
}

// strayImporters returns "pkg imports target" for every package outside allowed
// that imports one of targets, and every allowed package that imports none.
func strayImporters(g importGraph, targets []string, allowed map[string]string) []string {
	var out []string
	for _, pkg := range slices.Sorted(maps.Keys(g)) {
		hit := slices.ContainsFunc(g[pkg], func(d string) bool { return withinAny(d, targets) })
		if _, ok := allowed[pkg]; hit && !ok {
			out = append(out, pkg+" imports a trial package but is not on the allow-list")
		}
	}
	for _, pkg := range slices.Sorted(maps.Keys(allowed)) {
		if !slices.ContainsFunc(g[pkg], func(d string) bool { return withinAny(d, targets) }) {
			out = append(out, pkg+" is on the allow-list but imports no trial package: drop it")
		}
	}
	return out
}

// reachesTarget returns one import chain from every package under roots (minus
// the excluded packages) to a package under targets.
func reachesTarget(g importGraph, roots, excluded, targets []string) []string {
	var out []string
	for _, start := range slices.Sorted(maps.Keys(g)) {
		if !withinAny(start, roots) || withinAny(start, excluded) {
			continue
		}
		prev := map[string]string{start: ""}
		queue := []string{start}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, dep := range g[cur] {
				if _, seen := prev[dep]; seen {
					continue
				}
				prev[dep] = cur
				if withinAny(dep, targets) {
					chain := []string{dep}
					for p := cur; p != ""; p = prev[p] {
						chain = append([]string{p}, chain...)
					}
					out = append(out, strings.Join(chain, " -> "))
					continue
				}
				queue = append(queue, dep)
			}
		}
	}
	return out
}

// forbiddenImports returns "pkg imports dep" for every direct import of a
// forbidden package by a package under from.
func forbiddenImports(g importGraph, from, forbidden []string) []string {
	var out []string
	for _, pkg := range slices.Sorted(maps.Keys(g)) {
		if !withinAny(pkg, from) {
			continue
		}
		for _, dep := range g[pkg] {
			if withinAny(dep, forbidden) {
				out = append(out, pkg+" imports "+dep)
			}
		}
	}
	return out
}

func TestLivePathDoesNotImportTheTrial(t *testing.T) {
	g := loadImportGraph(t)
	for _, root := range slices.Concat(liveRoots, trialPackages, trialImportTargets) {
		if !slices.ContainsFunc(slices.Collect(maps.Keys(g)), func(p string) bool { return within(p, root) }) {
			t.Fatalf("%s has no Go sources: update this guard", root)
		}
	}
	for _, root := range liveRoots {
		if _, allowed := allowedTrialImporters[root]; allowed {
			t.Errorf("%s is a live root and must not be on the trial allow-list", root)
		}
	}
	for _, v := range strayImporters(g, trialImportTargets, allowedTrialImporters) {
		t.Error(v)
	}
	for _, chain := range reachesTarget(g, liveRoots, trialPackages, trialImportTargets) {
		t.Errorf("the live side reaches the trial: %s", chain)
	}
}

func TestTrialPackagesImportNoTradingOrOrderPlacementPackage(t *testing.T) {
	for _, v := range forbiddenImports(loadImportGraph(t), trialPackages, trialForbiddenImports) {
		t.Errorf("%s: the trial must not depend on trading, learning or order placement", v)
	}
}

// The checkers must be able to fail: run them on a synthetic graph (live ->
// trial directly, live -> handler -> trial, an unlisted importer, a stale
// allow-list entry, a trial package importing execution).
func TestImportGuardCheckersFailOnViolations(t *testing.T) {
	g := importGraph{
		"internal/service/risk":          {"internal/service/jev/trial"},
		"internal/service/policy":        {"internal/web/handler/settings"},
		"internal/web/handler/settings":  {"internal/service/jev/trialwire"},
		"internal/service/rag":           {"internal/domain"},
		"internal/service/jev/trial":     {"internal/service/jev/trialwire", "internal/service/execution/enrich"},
		"internal/service/jev/trialwire": nil,
		"internal/stale":                 {"internal/domain"},
		"internal/domain":                nil,
	}
	targets := []string{"internal/service/jev/trial", "internal/service/jev/trialwire"}
	allowed := map[string]string{"internal/web/handler/settings": "hint", "internal/service/jev/trial": "arms", "internal/stale": "gone"}
	got := strings.Join(strayImporters(g, targets, allowed), "\n")
	for _, want := range []string{"internal/service/risk imports a trial package", "internal/stale is on the allow-list"} {
		if !strings.Contains(got, want) {
			t.Errorf("strayImporters = %q, want it to mention %q", got, want)
		}
	}
	if strings.Contains(got, "internal/service/rag") || strings.Contains(got, "handler/settings imports") {
		t.Errorf("strayImporters flagged a clean or allowed package: %q", got)
	}
	live := []string{"internal/service/risk", "internal/service/policy", "internal/service/rag"}
	chains := reachesTarget(g, live, nil, targets)
	want := []string{
		"internal/service/policy -> internal/web/handler/settings -> internal/service/jev/trialwire",
		"internal/service/risk -> internal/service/jev/trial",
	}
	if !slices.Equal(chains, want) {
		t.Errorf("reachesTarget = %v, want %v", chains, want)
	}
	if got := reachesTarget(g, live[2:], nil, targets); len(got) != 0 {
		t.Errorf("a clean live package reaches the trial: %v", got)
	}
	bad := forbiddenImports(g, targets, []string{"internal/service/execution"})
	if !slices.Equal(bad, []string{"internal/service/jev/trial imports internal/service/execution/enrich"}) {
		t.Errorf("forbiddenImports = %v", bad)
	}
}
