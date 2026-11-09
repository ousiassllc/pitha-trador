package report_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/eligibility"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/labellayer"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest/manifesttest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/report"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/stats"
	"github.com/ousiassllc/pitha-trador/internal/service/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/policytest"
)

func f(v float64) *float64 { return &v }
func s(v string) *string   { return &v }

func frozen(t *testing.T) manifest.Manifest {
	t.Helper()
	m := manifesttest.Valid()
	if err := m.Freeze(m.CreatedAt); err != nil {
		t.Fatal(err)
	}
	return m
}

// labelResult runs the real label layer on two independent synthetic rows.
func labelResult(t *testing.T) labellayer.Result {
	t.Helper()
	model := policytest.LRModel(0, 1)
	mm := manifesttest.Valid()
	mm.Model = manifest.ModelRef{Version: model.Version, ParamsSHA256: manifest.ParamsHash("p")}
	if err := mm.Freeze(mm.CreatedAt); err != nil {
		t.Fatal(err)
	}
	var rows []labellayer.Row
	for i := range 4 {
		at := manifesttest.T0.AddDate(0, 0, 10+i/2).Add(9 * time.Hour)
		rows = append(rows, labellayer.Row{
			ID: int64(i + 1), DecidedAt: at, Cohort: "c", Direction: domain.JevDirectionLong, NetReturnBps: float64(10 * (i + 1)),
			Eligibility: eligibility.Input{Labeled: true, Policy: policy.Input{SpreadBps: f(3), Turnover5mJPY: f(1e7), Decision: &domain.JevDecision{
				Direction: s(domain.JevDirectionLong), Confidence: f(0.9), EntryQuality: s("strong"), ContinuationProbability: f(0.9), ToxicFlow: f(0.1), LiquidityStressed: f(0.1)}},
				Features: metalabel.Features{OFIShort: f(5), OFIMid: f(1), OFILong: f(1), MicropriceDevBps: f(1), SpreadBps: f(3), Return1m: f(0.001), JevScore: f(0.5), TDnetScore: f(0)}},
		})
	}
	res, err := labellayer.Run(context.Background(), labellayer.Input{Manifest: mm, Model: &model, ParamsSHA256: mm.Model.ParamsSHA256, Cohort: "c", Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func replay() report.ReplaySection {
	cell := func(n string) report.ReplayCell {
		return report.ReplayCell{Name: n, EligibleCandidates: 10, Entries: 4, PnLJPY: 1200, ReturnOnCapital: 0.0012, EndEquityJPY: 1001200,
			MaxDrawdownJPY: 300, MaxDrawdownRatio: 0.0003, Stages: []eligibility.StageDrop{{Stage: "risk", Count: 1}, {Stage: "fill", Count: 0}}, Daily: report.DailySummary{Days: 3, MeanJPY: 400, PositiveDays: 2}}
	}
	return report.ReplaySection{
		Available:          true,
		UnavailablePeriods: []report.UnavailablePeriod{{From: "2026-10-30", To: "2026-10-30", Reason: "market_snapshots missing"}},
		Segments: []report.ReplaySegment{{Name: "seg1", From: "2026-10-29", To: "2026-10-29", Days: 1, InitialState: manifest.InitialState{CapitalJPY: 1e6},
			Cells:       []report.ReplayCell{cell("m_off_q_off"), cell("m_on_q_on")},
			Differences: []stats.Result{{Name: "q_off_to_on@m_off", Verdict: stats.Undetermined, Reasons: []string{"unset:min_days"}}}}},
	}
}

func build(t *testing.T) report.Document {
	t.Helper()
	m := frozen(t)
	lr := labelResult(t)
	d, err := report.Build(report.Input{Manifest: m, Label: report.LabelSection{Available: true, Result: &lr}, Replay: replay()})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return d
}

func TestMarkdownStatesWhatAReaderMustKnow(t *testing.T) {
	md := report.Markdown(build(t))
	for _, want := range []string{
		"(i) 元のモデル評価（参考）", "(ii) 表 1: ラベル層", "(iii) 表 2: 再生層", "混ぜない",
		"proxy", "TP 30 bps / SL 30 bps", "10 分", "#774",
		"主比較", "q_off_to_on@m_off", "m_off_to_on@q_on", "対角", "参考",
		"優劣未確定", "別途の承認", "実行回数", "未設定の事前登録値",
		"最大 DD", "結合しない", "復元不能", "再生層が実行不可の期間", "market_snapshots missing",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("Markdown lacks %q", want)
		}
	}
}

func TestPrimaryPairsAreTheFourOneAxisComparisons(t *testing.T) {
	d := build(t)
	if len(d.PrimaryPairs) != 4 || d.Multiple.Registered != 8 || d.Multiple.Executed != 8 || d.Multiple.AlphaAdjusted != 0.05/8 {
		t.Fatalf("pairs %v multiple %+v", d.PrimaryPairs, d.Multiple)
	}
	if len(d.UnsetMinimums) != 5 || d.RunCount != 1 {
		t.Errorf("unset %v run %d", d.UnsetMinimums, d.RunCount)
	}
}

func TestFailedLayerShowsItsReasonAndTheOtherLayerSurvives(t *testing.T) {
	m := frozen(t)
	lr := labelResult(t)
	d, err := report.Build(report.Input{Manifest: m,
		Label:  report.LabelSection{Available: true, Result: &lr},
		Replay: report.ReplaySection{Reason: "market_snapshots missing for every day", UnavailablePeriods: []report.UnavailablePeriod{{From: "a", To: "b", Reason: "no snapshots"}}}})
	if err != nil {
		t.Fatal(err)
	}
	md := report.Markdown(d)
	if !strings.Contains(md, "実行不可") || !strings.Contains(md, "market_snapshots missing for every day") || !strings.Contains(md, "1 適格候補あたり") {
		t.Error("the failed replay layer or the surviving label layer is not shown")
	}
	if d.Multiple.Executed != 4 {
		t.Errorf("executed = %d, want only the label layer's 4", d.Multiple.Executed)
	}
	d2, err := report.Build(report.Input{Manifest: m, Label: report.LabelSection{Reason: "model params unreadable"}, Replay: replay()})
	if err != nil || !strings.Contains(report.Markdown(d2), "model params unreadable") || !strings.Contains(report.Markdown(d2), "再生層") {
		t.Errorf("failed label layer: %v", err)
	}
}

func TestBuildRejectsInconsistentInput(t *testing.T) {
	m := frozen(t)
	lr := labelResult(t)
	for name, in := range map[string]report.Input{
		"unfrozen manifest":                {Manifest: manifesttest.Valid(), Label: report.LabelSection{Reason: "x"}, Replay: report.ReplaySection{Reason: "y"}},
		"unavailable without why":          {Manifest: m, Label: report.LabelSection{}, Replay: report.ReplaySection{Reason: "y"}},
		"available without result":         {Manifest: m, Label: report.LabelSection{Available: true}, Replay: report.ReplaySection{Reason: "y"}},
		"more comparisons than registered": {Manifest: m, Label: report.LabelSection{Available: true, Result: &lr}, Replay: replay(), ExploratoryComparisons: 1},
	} {
		if _, err := report.Build(in); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOutputHasNoSecretsRawDataOrParamsBody(t *testing.T) {
	d := build(t)
	js, err := report.JSON(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{string(js), report.Markdown(d)} {
		if err := report.CheckContent(text); err != nil {
			t.Errorf("clean output rejected: %v", err)
		}
	}
	d.Replay.UnavailablePeriods[0].Reason = `{"state_json": {"bid": 1}}`
	if _, err := report.JSON(d); !errors.Is(err, report.ErrForbiddenContent) {
		t.Errorf("forbidden content passed: %v", err)
	}
	for _, bad := range []string{"response_json", "params_json", "API_KEY=abc", "Authorization: x"} {
		if report.CheckContent(bad) == nil {
			t.Errorf("%q was not detected", bad)
		}
	}
}

func TestWriteFilesToADirectoryOutsideTheDBAndNeverOverwrite(t *testing.T) {
	d := build(t)
	root := t.TempDir()
	dbDir := filepath.Join(root, "db-copy")
	if _, err := report.WriteFiles(filepath.Join(dbDir, "out"), d, dbDir); err == nil {
		t.Error("an output directory inside the DB copy's directory was accepted")
	}
	if _, err := report.WriteFiles("", d); err == nil {
		t.Error("empty directory accepted")
	}
	out := filepath.Join(root, "out", "nested")
	files, err := report.WriteFiles(out, d, dbDir)
	if err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	for _, p := range []string{files.JSON, files.Markdown, files.Manifest} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
	if _, err := manifest.ReadFrozen(files.Manifest); err != nil {
		t.Errorf("the written manifest is not a valid frozen one: %v", err)
	}
	if _, err := report.WriteFiles(out, d, dbDir); err == nil {
		t.Error("a second run overwrote the first run's files")
	}
}
