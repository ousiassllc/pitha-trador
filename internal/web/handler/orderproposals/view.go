package orderproposals

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
	"github.com/ousiassllc/pitha-trador/internal/web/labels"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// The Templ components take plain props, so a change to the proposal read
// model surfaces here (docs/components/overview.md §3). Nothing is recomputed:
// every number is shown as the read model has it (the realized pnl is
// positions.realized_pnl as stored).

func rows(items []proposal.Item) []pages.ProposalRow {
	out := make([]pages.ProposalRow, len(items))
	for i, it := range items {
		out[i] = pages.ProposalRow{
			ID: it.ProposalID, ReceivedAt: jst(it.ReceivedAt), Symbol: it.Symbol, Bucket: it.Bucket, Action: it.Action,
			Status: it.Status, StatusReason: reasonName(it.StatusReason),
			Quantity: quantities(it.Quantity, it.BudgetJPY, it.AppliedQuantity),
			MaxLoss:  optYen(it.MaxLossJPY), EstimatedLoss: optYen(it.ComputedMaxLossJPY),
			RealizedPnl: optYen(it.RealizedPnlJPY), PnlSign: sign(it.RealizedPnlJPY),
		}
	}
	return out
}

func detail(s proposal.State) pages.ProposalDetailProps {
	p := pages.ProposalDetailProps{
		ID: s.ProposalID, Symbol: s.Symbol, Bucket: s.Bucket, Action: s.Action, OrderType: named(orderTypeNames, s.OrderType),
		Status: s.Status, StatusReason: reasonName(s.StatusReason), Rationale: s.Rationale, ExitPlan: deref(s.ExitPlan),
	}
	p.Summary = []pages.ProposalField{
		{Label: "受付（JST）", Value: jst(s.ReceivedAt)},
		{Label: "最終更新（JST）", Value: jst(s.UpdatedAt)},
		{Label: "有効期限（JST）", Value: jst(s.ExpiresAt)},
		{Label: "取消の依頼（JST）", Value: optJST(s.CancelRequestedAt)},
		{Label: "数量（提案）", Value: optShares(s.Quantity)},
		{Label: "予算（提案）", Value: optYen(s.BudgetJPY)},
		{Label: "指値", Value: optYen(s.LimitPrice)},
		{Label: "数量（適用）", Value: optShares(s.AppliedQuantity), Help: "提案の数量・予算をもとに、アプリが決めた数量です。"},
	}
	r := s.Result
	p.Plan = []pages.ProposalField{
		{Label: "損切り価格（案）", Value: optYen(s.StopLossPrice)},
		{Label: "利確価格（案）", Value: optYen(s.TakeProfitPrice)},
		{Label: "適用された損切り価格", Value: optYen(s.EffectiveStopLossPrice), Help: "提案の損切り価格と、アプリが定めた損切り価格のうち、遠い方です。"},
		{Label: "決済の約定は損切りに届いたか", Value: reached(r.StopLossReached)},
		{Label: "決済の約定は利確に届いたか", Value: reached(r.TakeProfitReached)},
	}
	p.Loss = []pages.ProposalField{
		{Label: "AIの見込み", Value: optYen(r.MaxLoss.ProposedJPY), Help: "AIが提案で見込んだ最大損失です。"},
		{Label: "アプリの見積り", Value: optYen(r.MaxLoss.EstimatedJPY), Help: "数量 ×（約定見込み価格 − 損切り価格）で見積もった最大損失です。"},
		{Label: "実際の損失", Value: optYen(r.MaxLoss.ActualLossJPY), Help: "決済済みのポジションの実現損失です。利益なら 0 です。"},
	}
	for _, src := range s.Sources {
		p.Sources = append(p.Sources, pages.ProposalSource{
			Title: src.Title, URL: src.URL, Href: linkHref(src.URL), RetrievedAt: jst(src.RetrievedAt),
		})
	}
	for _, e := range s.Events {
		p.Events = append(p.Events, pages.ProposalEvent{
			At: jst(e.At), From: statusName(deref(e.FromStatus)), To: statusName(e.ToStatus), Reason: reasonName(e.ReasonCode), Actor: labels.Actor(e.Actor), Detail: labels.EventDetail(e.Detail),
		})
	}
	p.Linked = linked(r, s.Status)
	return p
}

// linked is the records the application linked to the proposal, in the order
// they come about: trade signal -> paper order -> position.
func linked(r proposal.Result, status string) []pages.ProposalLinked {
	var out []pages.ProposalLinked
	if sg := r.Signal; sg != nil {
		risk := pages.ProposalField{Label: "リスク判定", Value: passed(sg.RiskPassed)}
		reject := pages.ProposalField{Label: "拒否の理由", Value: reasonName(deref(sg.RejectCode))}
		if sg.RejectedAfterRisk { // passed Risk, refused by the application afterwards
			risk.Value, reject.Label = laterRejection, "理由"
		} else if end, ok := afterRiskEnd[status]; ok && !sg.RiskPassed { // passed Risk, no order came of it for another reason
			risk.Value, reject.Label = "リスク判定は通過・"+end, "理由"
		}
		out = append(out, pages.ProposalLinked{Title: labels.Numbered("トレードシグナル", sg.ID), Fields: []pages.ProposalField{
			{Label: "時刻（JST）", Value: jst(sg.At)},
			{Label: "ポリシーのバージョン", Value: sg.PolicyVersion, Help: "このシグナルを出した時点のポリシーのバージョンです。"},
			risk, reject,
		}})
	}
	if o := r.Order; o != nil {
		out = append(out, pages.ProposalLinked{Title: labels.Numbered("注文", o.ID), Fields: []pages.ProposalField{
			{Label: "売買 / 種別", Value: named(sideNames, o.Side) + " / " + named(orderTypeNames, o.OrderType)},
			{Label: "状態", Value: named(orderStatusNames, o.Status)},
			{Label: "数量", Value: shares(o.Quantity)},
			{Label: "発注（JST）", Value: jst(o.SubmittedAt)},
			{Label: "約定（JST）", Value: optJST(o.FilledAt)},
			{Label: "約定価格", Value: optYen(o.FilledPrice)},
			{Label: "手数料", Value: yen(o.FeesJPY)},
		}})
	}
	if pos := r.Position; pos != nil {
		status := "保有中"
		if pos.Status == "closed" {
			status = "決済済み"
		}
		out = append(out, pages.ProposalLinked{Title: labels.Numbered("ポジション", pos.ID), Fields: []pages.ProposalField{
			{Label: "状態", Value: status},
			{Label: "数量", Value: shares(pos.Quantity)},
			{Label: "建値", Value: yen(pos.EntryPrice)},
			{Label: "建玉（JST）", Value: jst(pos.OpenedAt)},
			{Label: "決済（JST）", Value: optJST(pos.ClosedAt)},
			{Label: "決済価格", Value: optYen(pos.ExitPrice)},
			{Label: "決済理由", Value: exitReasonName(pos.ExitReason)},
			{Label: "保有時間", Value: minutes(pos.HoldingMinutes)},
			{Label: "手数料", Value: optYen(pos.FeesJPY), Help: "買いと売りの合計です。"},
			{Label: "実現損益", Value: optYen(pos.RealizedPnl), Sign: sign(pos.RealizedPnl)},
		}})
	}
	return out
}

// linkHref is the link target of a source URL: only an absolute http or https
// URL becomes one; anything else (javascript:, data:, a relative path, ...) is
// shown as text.
func linkHref(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.String()
}

// The order's side, type and status are stored as codes (BUY, MARKET, FILLED,
// ...); the screens show them by name. A code not listed is shown as it is.
var (
	sideNames        = map[string]string{"BUY": "買い", "SELL": "売り"}
	orderTypeNames   = map[string]string{"MARKET": "成行", "LIMIT": "指値"}
	orderStatusNames = map[string]string{"PENDING": "発注中", "FILLED": "約定", "CANCELLED": "取消", "REJECTED": "拒否"}
)

// named is the name of a code (case-insensitive), labels.Other when unknown.
func named(names map[string]string, code string) string {
	if n, ok := names[strings.ToUpper(code)]; ok {
		return n
	}
	return labels.Other
}

// statusName is the name of a proposal status code, "" for none.
func statusName(code string) string {
	if code == "" {
		return ""
	}
	return pages.ProposalStatusLabel(code)
}

func jst(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return atoms.FormatJST(t)
}

func optJST(rfc3339 *string) string {
	if rfc3339 == nil {
		return ""
	}
	return jst(*rfc3339)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func yen(v float64) string { return dailyguardsettings.FormatYen(v) + " 円" }

func optYen(v *float64) string {
	if v == nil {
		return ""
	}
	return yen(*v)
}

func shares(n int64) string { return strconv.FormatInt(n, 10) + " 株" }

func optShares(n *int64) string {
	if n == nil {
		return ""
	}
	return shares(*n)
}

// quantities is the list's 数量 cell: the proposed shares (or budget) and, once
// the application decided, the shares it applied.
func quantities(qty *int64, budget *float64, applied *int64) string {
	var proposed string
	switch {
	case qty != nil:
		proposed = strconv.FormatInt(*qty, 10)
	case budget != nil:
		proposed = "予算 " + yen(*budget)
	}
	switch {
	case applied == nil:
		return proposed
	case proposed == "":
		return strconv.FormatInt(*applied, 10)
	}
	return proposed + " → " + strconv.FormatInt(*applied, 10)
}

// sign is "gain" or "loss" for a non-zero pnl, "" otherwise.
func sign(v *float64) string {
	switch {
	case v == nil || *v == 0:
		return ""
	case *v > 0:
		return "gain"
	}
	return "loss"
}

func reached(b *bool) string {
	switch {
	case b == nil:
		return ""
	case *b:
		return "届いた"
	}
	return "届かなかった"
}

// laterRejection is the signal's Risk check when the proposal passed
// the Risk Engine and the application refused it afterwards.
const laterRejection = "リスク判定は通過・後段で拒否"

// afterRiskEnd is how a proposal that passed Risk but did not become an order
// is shown when it did not end as a later rejection: by what became of it.
var afterRiskEnd = map[string]string{
	"expired": "期限切れ", "cancelled": "取消", "waiting_session": "セッション待ち",
	"received": "再処理待ち", "failed": "失敗",
}

func passed(ok bool) string {
	if ok {
		return "通過"
	}
	return "拒否"
}

func minutes(m *int64) string {
	if m == nil {
		return ""
	}
	return strconv.FormatInt(*m/60, 10) + " 時間 " + strconv.FormatInt(*m%60, 10) + " 分"
}

// reasonName is the name of a reason code; "" (no reason) stays "".
func reasonName(code string) string {
	if code == "" {
		return ""
	}
	return labels.ProposalReason(code)
}

// exitReasonName is the name of a position's exit reason; nil (still open) is "".
func exitReasonName(code *string) string {
	if code == nil || *code == "" {
		return ""
	}
	return labels.ExitReason(*code)
}
