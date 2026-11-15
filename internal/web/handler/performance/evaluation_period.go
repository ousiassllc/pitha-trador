package performance

import (
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/actuals"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/evaluation"
)

// Query parameters of the 実績評価 period selector. They are not `from`/`to`:
// a `from` query selects the バックテスト tab (Page).
const (
	evalFromParam = "eval_from"
	evalToParam   = "eval_to"
)

// evaluationPresetDays are the preset ranges of the selector: the last N JST
// days including today.
var evaluationPresetDays = []struct {
	label string
	days  int
}{{"直近7日", 7}, {"直近30日", 30}, {"直近90日", 90}}

// parseEvaluationPeriod reads the 実績評価 period (the closed-at JST days
// eval_from..eval_to, the end inclusive; either may be omitted) into the
// range to evaluate and the selector's props. Invalid input returns the
// Japanese message to show and evaluates the whole history instead (the form
// echoes the raw input).
func parseEvaluationPeriod(c *gin.Context, now time.Time) (evaluation.Period, actuals.Range, string) {
	p := evaluation.Period{From: c.Query(evalFromParam), To: c.Query(evalToParam)}
	p.Links = periodLinks(p, now)

	var (
		r    actuals.Range
		errs string
	)
	if p.From != "" {
		from, err := time.ParseInLocation(dateLayout, p.From, jst)
		if err != nil {
			errs = "評価期間の開始日は YYYY-MM-DD 形式の日付で指定してください。"
		}
		r.From = from
	}
	if p.To != "" {
		to, err := time.ParseInLocation(dateLayout, p.To, jst)
		switch {
		case err != nil && errs == "":
			errs = "評価期間の終了日は YYYY-MM-DD 形式の日付で指定してください。"
		case err == nil:
			r.To = to.AddDate(0, 0, 1)
		}
	}
	if errs == "" && !r.From.IsZero() && !r.To.IsZero() && !r.To.After(r.From) {
		errs = "評価期間の終了日は開始日以降の日付で指定してください。"
	}
	if errs != "" {
		return p, actuals.Range{}, errs
	}
	return p, r, ""
}

// periodLinks returns the "全期間" and preset links, marking the one equal to
// the current input.
func periodLinks(cur evaluation.Period, now time.Time) []evaluation.PeriodLink {
	today := now.In(jst)
	links := []evaluation.PeriodLink{{Label: "全期間", Href: "/performance", Current: cur.From == "" && cur.To == ""}}
	for _, preset := range evaluationPresetDays {
		from := today.AddDate(0, 0, 1-preset.days).Format(dateLayout)
		to := today.Format(dateLayout)
		q := url.Values{evalFromParam: {from}, evalToParam: {to}}
		links = append(links, evaluation.PeriodLink{
			Label: preset.label, Href: "/performance?" + q.Encode(), Current: cur.From == from && cur.To == to,
		})
	}
	return links
}
