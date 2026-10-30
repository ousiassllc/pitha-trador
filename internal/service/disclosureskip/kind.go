package disclosureskip

import "strings"

// Kind is the kind of disclosure the skip rule reacts to.
type Kind string

// The kinds the rule covers. Other disclosures (dividends only, a bond
// redemption, ...) have no Kind and never match.
const (
	KindEarnings         Kind = "earnings"          // 決算短信
	KindForecastRevision Kind = "forecast_revision" // 業績予想の修正（上方・下方とも）
)

// Kinds lists every Kind, in the order reports show them.
var Kinds = []Kind{KindEarnings, KindForecastRevision}

// kindRules is the one place that says which titles are which kind: a title
// is of a kind when it contains every word of at least one of the kind's word
// groups. Titles are the やのしん / TDnet headlines, e.g.
//
//	2026年3月期 第2四半期（中間期）決算短信〔日本基準〕（連結）  -> earnings
//	業績予想の修正に関するお知らせ / 通期業績予想の上方修正…       -> forecast_revision
//	業績予想及び配当予想の修正に関するお知らせ                   -> forecast_revision
//	配当予想の修正に関するお知らせ                               -> none (dividend only)
//
// Changing a list changes what the rule means: bump RuleVersion with it.
var kindRules = []struct {
	kind   Kind
	groups [][]string
}{
	{KindEarnings, [][]string{{"決算短信"}}},
	{KindForecastRevision, [][]string{{"業績予想", "修正"}}},
}

// Classify returns the Kind of a disclosure title and whether the skip rule
// covers it at all. A title of several kinds is the first of Kinds.
func Classify(title string) (Kind, bool) {
	for _, rule := range kindRules {
		for _, words := range rule.groups {
			if containsAll(title, words) {
				return rule.kind, true
			}
		}
	}
	return "", false
}

func containsAll(title string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(title, w) {
			return false
		}
	}
	return true
}
