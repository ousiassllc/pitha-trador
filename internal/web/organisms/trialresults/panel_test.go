package trialresults_test

import (
	"bytes"
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/ousiassllc/pitha-trador/internal/web/organisms/trialresults"
)

func renderPage(t *testing.T, c templ.Component) string {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func f(v float64) *float64 { return &v }

func trialProps() trialresults.Props {
	return trialresults.Props{
		Periods:    []trialresults.PeriodLink{{Label: "直近 7 日", Href: "/performance/trial?days=7"}, {Label: "直近 30 日", Href: "/performance/trial?days=30", Current: true}},
		CohortSize: 4,
		Arms: []trialresults.Arm{
			{ID: "arm-a", Calls: 8, Missing: 1, MissingRate: f(0.125), Skipped: 2},
			{ID: "arm-skip-only", Skipped: 3},
		},
		SkipReasons: []trialresults.SkipRow{{ArmID: "arm-a", Reason: "同時に呼べる数がいっぱいだった", Count: 2}},
		Cost: []trialresults.CostRow{
			{Label: "arm-a", Reported: trialresults.SourceCost{Calls: 5, USD: 0.0123}, Estimate: trialresults.SourceCost{Calls: 2, USD: 0.004}, UnknownCalls: 3},
			{Label: "合計", Total: true, Reported: trialresults.SourceCost{Calls: 5, USD: 0.0123}, UnknownCalls: 3},
		},
		Proxy: []trialresults.ProxyRow{{ArmID: "arm-a", NoneAnswers: 2, NoAnswer: 1, Horizons: []trialresults.ProxyHorizon{{Minutes: 5, Labelled: 2, Coverage: f(2.0 / 3), TP: 1, SL: 1, AvgNetBps: f(-3.5)}}}},
	}
}

func visibleText(body string) string {
	return html.UnescapeString(regexp.MustCompile(`(?s)<[^>]*>`).ReplaceAllString(body, " "))
}

// The お試し tab of 成績 is read-only: no form, button or link to anything that
// writes, and nothing that names a winner, vote or promotion.
func TestPanel_IsReadOnlyAndNamesNoWinner(t *testing.T) {
	for name, props := range map[string]trialresults.Props{"figures": trialProps(), "empty": {Empty: true}, "error": {Error: "お試しの成績を読み込めませんでした。"}} {
		body := renderPage(t, trialresults.Panel(props))
		page := body
		for _, banned := range []string{"<form", "<input", "<select", "<textarea", `type="submit"`, "hx-post", "hx-put", "hx-delete", `method="post"`, `method="POST"`, "formaction"} {
			if strings.Contains(page, banned) {
				t.Errorf("%s: the page holds %q: %s", name, banned, page)
			}
		}
		// The only <button>s are the HelpMark's (type=button).
		for _, m := range regexp.MustCompile(`<button\b[^>]*>`).FindAllString(page, -1) {
			if !strings.Contains(m, `type="button"`) || !strings.Contains(m, "data-help-mark-button") {
				t.Errorf("%s: a button that is not a help mark: %s", name, m)
			}
		}
		text := visibleText(page)
		for _, banned := range []string{"勝者", "多数決", "合議", "昇格", "推奨", "採用", "winner", "vote", "consensus", "promot", "順位"} {
			// The help text may only deny them ("順位や勝者は出していません").
			for _, sentence := range strings.Split(text, "。") {
				if strings.Contains(sentence, banned) && !strings.Contains(sentence, "出していません") {
					t.Errorf("%s: %q appears outside a denial: %q", name, banned, sentence)
				}
			}
		}
		for _, dev := range regexp.MustCompile(`(?i)issue\s*#?\d+|\bPR\s*#?\d+|#\d{3,}|jev_trial_|jev\.trial|OPENROUTER`).FindAllString(text, -1) {
			t.Errorf("%s: a dev note on the screen: %q", name, dev)
		}
	}
}

func TestPanel_EmptyStateIsShortAndHasNoTables(t *testing.T) {
	body := renderPage(t, trialresults.Panel(trialresults.Props{Empty: true}))
	if !strings.Contains(body, `data-testid="trial-empty"`) || !strings.Contains(body, "お試しの記録はまだありません。") {
		t.Errorf("no empty state: %s", body)
	}
	for _, absent := range []string{"<table", `data-testid="trial-cost"`, `data-testid="trial-proxy"`} {
		if strings.Contains(body, absent) {
			t.Errorf("an empty trial shows %q", absent)
		}
	}
	if !strings.Contains(body, `id="help-trial-empty"`) {
		t.Errorf("the detail of the empty state must be behind a help mark")
	}
}

// An unknown cost is a count ("不明（3 件）"), never an amount, so it can not read as 0 yen / USD.
func TestPanel_UnknownCostIsACountNeverZero(t *testing.T) {
	body := renderPage(t, trialresults.Panel(trialProps()))
	cost := body[strings.Index(body, `data-testid="trial-cost"`):]
	cost = cost[:strings.Index(cost, "</section>")]
	cell := regexp.MustCompile(`(?s)data-metric="unknown">([^<]*)<`).FindAllStringSubmatch(cost, -1)
	if len(cell) != 2 || cell[0][1] != "不明（3 件）" || cell[1][1] != "不明（3 件）" {
		t.Fatalf("unknown cells = %v, want 不明（3 件） on every row", cell)
	}
	for _, m := range cell {
		if strings.ContainsAny(m[1], "$￥円") || strings.Contains(m[1], "0.00") {
			t.Errorf("unknown cell reads as an amount: %q", m[1])
		}
	}
	for _, want := range []string{"$0.0123（5 件）", "$0.0040（2 件）", "報告された費用", "定価からの推定"} {
		if !strings.Contains(cost, want) {
			t.Errorf("cost table lacks %q: %s", want, cost)
		}
	}
	// No call of a source reads "—", and an arm with no unknown call reads "なし" (not 不明).
	zero := trialProps()
	zero.Cost = []trialresults.CostRow{{Label: "arm-b", Reported: trialresults.SourceCost{Calls: 1, USD: 1}}}
	b := renderPage(t, trialresults.Panel(zero))
	if !strings.Contains(b, `data-metric="list_price_estimate">—<`) || !strings.Contains(b, `data-metric="unknown">なし<`) {
		t.Errorf("source without a call must read — / なし: %s", b)
	}
}

func TestPanel_MissingRateAndSkipsAreShown(t *testing.T) {
	body := renderPage(t, trialresults.Panel(trialProps()))
	for _, want := range []string{`data-arm="arm-a"`, "12.5%（1 / 8）", `data-metric="skipped">2<`, `data-testid="trial-skips"`, "同時に呼べる数がいっぱいだった", "全アームに聞けた判断: 4 件"} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q: %s", want, body)
		}
	}
	// A skip-only arm has no call: its missing rate is "—", never 0%.
	row := body[strings.Index(body, `data-arm="arm-skip-only"`):]
	row = row[:strings.Index(row, "</tr>")]
	if !strings.Contains(row, `data-metric="missing_rate">—<`) || strings.Contains(row, "0.0%") {
		t.Errorf("an arm without a call must show — as the missing rate: %s", row)
	}
	// No skip at all: no skip section.
	p := trialProps()
	p.SkipReasons = nil
	if strings.Contains(renderPage(t, trialresults.Panel(p)), `data-testid="trial-skips"`) {
		t.Errorf("skip section without a skip")
	}
}

// NONE and the counterfactual labels are a proxy: the heading says 参考値・proxy
// and the help text says they are not a realized outcome.
func TestPanel_ProxyIsLabelledAsReference(t *testing.T) {
	body := renderPage(t, trialresults.Panel(trialProps()))
	section := body[strings.Index(body, `data-testid="trial-proxy"`):]
	section = section[:strings.Index(section, "</section>")]
	for _, want := range []string{"NONE と反実仮想（参考値・proxy）", "平均（参考値）", "実際の結果ではなく", "66.7%（2 件）", "-3.5 bps", `data-metric="none_answers">2<`, `data-metric="no_answer">1<`} {
		if !strings.Contains(section, want) {
			t.Errorf("proxy section lacks %q: %s", want, section)
		}
	}
}

func TestPanel_ErrorReplacesTheFigures(t *testing.T) {
	body := renderPage(t, trialresults.Panel(trialresults.Props{Error: "お試しの成績を読み込めませんでした。", Periods: trialProps().Periods}))
	if !strings.Contains(body, `data-testid="trial-error"`) || strings.Contains(body, "<table") || strings.Contains(body, `data-testid="trial-empty"`) {
		t.Errorf("error state: %s", body)
	}
}
