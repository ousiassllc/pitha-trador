package labels_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/labels"
)

// repoRoot is the module root, found from this test's directory (internal/web/labels).
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return filepath.Join(wd, "..", "..", "..")
}

// constValues is the string value of every const in the non-test Go files of
// dir (relative to the repo root) whose name matches name.
func constValues(t *testing.T, dir string, name *regexp.Regexp) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(filepath.Join(repoRoot(t), dir))
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(repoRoot(t), dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatalf("parse %s/%s: %v", dir, e.Name(), err)
		}
		files = append(files, f)
	}
	var out []string
	{
		for _, file := range files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					for i, id := range vs.Names {
						if !name.MatchString(id.Name) || i >= len(vs.Values) {
							continue
						}
						lit, ok := vs.Values[i].(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						v, err := strconv.Unquote(lit.Value)
						if err != nil {
							t.Fatalf("%s.%s: %v", dir, id.Name, err)
						}
						out = append(out, v)
					}
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no const matching %v in %s: the test lost its source", name, dir)
	}
	return out
}

// requireCovers fails for every code of the domain that the table has no name for.
func requireCovers(t *testing.T, what string, table map[string]string, codes []string) {
	t.Helper()
	for _, c := range codes {
		if table[c] == "" {
			t.Errorf("%s: code %q has no screen label: add it to internal/web/labels (and static/src/components/lib/labels.ts)", what, c)
		}
	}
}

var (
	reasonConst = regexp.MustCompile(`^Reason`)
	rejectConst = regexp.MustCompile(`^Reject`)
)

// The label tables cover every value the domain can store: a code added later
// fails here instead of reaching a screen as English.
func TestLabelTables_CoverEveryDomainCode(t *testing.T) {
	requireCovers(t, "exit reasons", labels.ExitReasons, constValues(t, "internal/domain", regexp.MustCompile(`^ExitReason`)))
	requireCovers(t, "kill switch reasons", labels.KillReasons, constValues(t, "internal/domain", regexp.MustCompile(`^KillReason`)))
	requireCovers(t, "kill switch resolved_by", labels.ResolvedBys, constValues(t, "internal/domain", regexp.MustCompile(`^ResolvedBy`)))
	requireCovers(t, "activity types", labels.ActivityTypes, constValues(t, "internal/domain", regexp.MustCompile(`^ActivityType`)))
	requireCovers(t, "job queues", labels.JobQueues, constValues(t, "internal/repository/jobqueue", regexp.MustCompile(`^JobQueue`)))
	requireCovers(t, "job statuses", labels.JobStatuses, constValues(t, "internal/repository/jobqueue", regexp.MustCompile(`^JobStatus`)))
	requireCovers(t, "retraining skips", labels.RetrainSkips, constValues(t, "internal/repository/metalabel", regexp.MustCompile(`^Skip`)))
	requireCovers(t, "A/B arms", labels.ArmNames, constValues(t, "internal/domain/marketregime", regexp.MustCompile(`^Arm`)))
	var secretKeys []string
	for _, v := range constValues(t, "internal/config", regexp.MustCompile(`^Key`)) {
		if regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`).MatchString(v) { // the secrets-table keys, not the lower-case setting keys
			secretKeys = append(secretKeys, v)
		}
	}
	requireCovers(t, "secret keys", labels.ConnectionItemNames, secretKeys)
	requireCovers(t, "order proposal actors", labels.Actors, constValues(t, "internal/domain/orderproposal", regexp.MustCompile(`^Actor`)))

	// Policy Engine reject_reason heads (+ the meta-labeling and regime ones).
	policy := constValues(t, "internal/service/policy", reasonConst)
	requireCovers(t, "policy reject reasons", labels.RejectReasons, policy)

	// order proposal reason codes: the processor's, the Risk Engine's and the daily halts'.
	for _, dir := range []string{
		"internal/bootstrap/proposalexec", "internal/service/risk/reason", "internal/service/risk/correlation",
		"internal/service/risk/sizing", "internal/service/risk/bucket",
	} {
		requireCovers(t, "order proposal reasons in "+dir, labels.ProposalReasons, constValues(t, dir, reasonConstFor(dir)))
	}
	requireCovers(t, "daily halt reasons", labels.ProposalReasons, constValues(t, "internal/domain", regexp.MustCompile(`^DailyHaltReason`)))
}

// reasonConstFor is the name pattern of the reason codes in one package: the
// Risk Engine's leaf package "reason" names them without a prefix.
func reasonConstFor(dir string) *regexp.Regexp {
	if strings.HasSuffix(dir, "/reason") {
		return regexp.MustCompile(`.`)
	}
	return reasonConst
}

// An activity detail is shown with its keys and codes named; one of a shape
// this screen does not know is not printed.
func TestActivityDetail_UnknownShapeIsNotPrinted(t *testing.T) {
	cases := []struct{ typ, detail, want string }{
		{"job", "queue=market-data status=failed attempts=2 error=boom", "市況データの取得・失敗・試行 2 回・エラー: boom"},
		{"job", "queue=market-data status=succeeded attempts=1 note=skipped: x", "市況データの取得・成功・試行 1 回・メモ: skipped: x"},
		{"job", "queue=brand-new status=running attempts=1", "その他・実行中・試行 1 回"},
		{"job", "something=else", "（詳細を表示できません）"},
		{"jev_scout", "question_version=q1", "質問の版: q1"},
		{"jev_trader", "direction=LONG confidence=0.70", "方向: LONG・確信度: 0.70"},
		{"kill_switch", "reason=daily_loss_limit resolved_by=auto", "理由: 日次の損失上限に達した（解除: 自動）"},
		{"kill_switch", "reason=daily_loss_limit", "理由: 日次の損失上限に達した（未解除）"},
		{"kill_switch", "reason=brand_new_reason resolved_by=manual", "理由: その他（理由コードあり）（解除: 手動）"},
		{"news_feed", "news fetch failed: timeout", "news fetch failed: timeout"},
	}
	for _, c := range cases {
		if got := labels.ActivityDetail(c.typ, c.detail); got != c.want {
			t.Errorf("ActivityDetail(%q, %q) = %q, want %q", c.typ, c.detail, got, c.want)
		}
	}
}

// A code the tables do not know reads as the generic Japanese name, never as the code.
func TestLabels_UnknownCodeIsReplacedNotPrinted(t *testing.T) {
	const code = "totally_new_code"
	got := map[string]string{
		"exit":     labels.ExitReason(code),
		"kill":     labels.KillReason(code),
		"skip":     labels.RetrainSkip(code),
		"reject":   labels.RejectReason(code),
		"proposal": labels.ProposalReason(code),
	}
	for kind, name := range got {
		if name != labels.Unknown || strings.Contains(name, code) {
			t.Errorf("%s: unknown code shown as %q, want %q", kind, name, labels.Unknown)
		}
	}
	for kind, name := range map[string]string{
		"activity type": labels.ActivityType(code), "job queue": labels.JobQueue(code), "job status": labels.JobStatus(code),
		"actor": labels.Actor(code), "arm": labels.ABArm(code), "resolved_by": labels.ResolvedBy(code),
	} {
		if name != labels.Other {
			t.Errorf("%s: unknown code shown as %q, want %q", kind, name, labels.Other)
		}
	}
}

func TestEventDetail_NamesKnownKeysAndCountsTheRest(t *testing.T) {
	cases := []struct{ raw, want string }{
		{``, ""},
		{`null`, ""},
		{`{}`, ""},
		{`{"requested_quantity":300,"applied_quantity":100,"limited_by":"position_size"}`, "適用した数量: 100・数量を減らした理由: ポジションサイズの上限・提案の数量: 300"},
		{`{"recovered_after_restart":true,"trade_signal_id":7}`, "再起動後に復旧: はい・トレードシグナル: 7 番"},
		{`{"latest_price":2500.5,"mystery":1,"other":2}`, "直近の価格: 2500.5・ほか 2 項目"},
		{`{"limited_by":"a_new_limit"}`, "数量を減らした理由: その他（理由コードあり）"},
	}
	for _, c := range cases {
		if got := labels.EventDetail([]byte(c.raw)); got != c.want {
			t.Errorf("EventDetail(%s) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestNumbered_HasNoHash(t *testing.T) {
	if got := labels.Numbered("注文提案", 5); got != "注文提案 5 番" {
		t.Errorf("Numbered = %q", got)
	}
}

// tsTable is the `NAME = { 'key': 'value', ... }` object literal of static/src/components/lib/labels.ts.
func tsTable(t *testing.T, src, name string) map[string]string {
	t.Helper()
	block := regexp.MustCompile(`(?s)export const ` + name + `: Record<string, string> = \{(.*?)\n\};`).FindStringSubmatch(src)
	if block == nil {
		t.Fatalf("table %s not found in lib/labels.ts", name)
	}
	out := map[string]string{}
	for _, m := range regexp.MustCompile(`(?:'([^']+)'|([A-Za-z_]+)):\s*'([^']*)'`).FindAllStringSubmatch(block[1], -1) {
		out[m[1]+m[2]] = m[3]
	}
	return out
}

// The Lit components print the TypeScript tables and the server-rendered
// screens the Go ones; they must say the same thing.
func TestTypeScriptLabelsMatchGo(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "static", "src", "components", "lib", "labels.ts"))
	if err != nil {
		t.Fatalf("read lib/labels.ts: %v", err)
	}
	src := string(b)
	for _, c := range []struct {
		ts  string
		go_ map[string]string
	}{
		{"EXIT_REASONS", labels.ExitReasons},
		{"KILL_REASONS", labels.KillReasons},
		{"RESOLVED_BYS", labels.ResolvedBys},
		{"RETRAIN_SKIPS", labels.RetrainSkips},
		{"ACTIVITY_TYPES", labels.ActivityTypes},
		{"JOB_QUEUES", labels.JobQueues},
		{"JOB_STATUSES", labels.JobStatuses},
		{"MODEL_ALGORITHMS", labels.ModelAlgorithms},
	} {
		ts := tsTable(t, src, c.ts)
		if !maps.Equal(ts, c.go_) {
			t.Errorf("%s differs between lib/labels.ts and internal/web/labels:\n ts only/different: %v\n go only/different: %v", c.ts, diff(ts, c.go_), diff(c.go_, ts))
		}
	}
	// every machine reject reason of the nightly retraining has a name too
	requireCovers(t, "model reject reasons (lib/labels.ts)", tsTable(t, src, "MODEL_REJECT_REASONS"),
		constValues(t, "internal/service/metalabel/training", rejectConst))
}

func diff(a, b map[string]string) []string {
	var out []string
	for k, v := range a {
		if b[k] != v {
			out = append(out, k+"="+v)
		}
	}
	slices.Sort(out)
	return out
}

// The cutoff warning is shared with the API, the MCP and the log, so its
// wording stays; the screen only names the setting key in it.
func TestScreenWarning_NamesTheKeyAndKeepsTheWarning(t *testing.T) {
	shared := "学習データの終わりの根拠が未記録です。確認した Jev のモデル名を metalabel.llm_cutoff_model に記録してください。"
	got := labels.ScreenWarning(shared)
	if strings.Contains(got, "metalabel.") {
		t.Errorf("ScreenWarning keeps a setting key: %q", got)
	}
	if !strings.Contains(got, "学習データの終わりの根拠が未記録です。") || !strings.Contains(got, "に記録してください。") {
		t.Errorf("ScreenWarning lost the warning: %q", got)
	}
	if labels.ScreenWarning("") != "" {
		t.Error("an empty warning must stay empty")
	}
}
