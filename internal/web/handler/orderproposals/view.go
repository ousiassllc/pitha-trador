package orderproposals

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/dailyguardsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/proposal"
	"github.com/ousiassllc/pitha-trador/internal/web/atoms"
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
			Status: it.Status, StatusReason: it.StatusReason,
			Quantity: quantities(it.Quantity, it.BudgetJPY, it.AppliedQuantity),
			MaxLoss:  optYen(it.MaxLossJPY), EstimatedLoss: optYen(it.ComputedMaxLossJPY),
			RealizedPnl: optYen(it.RealizedPnlJPY), PnlSign: sign(it.RealizedPnlJPY),
		}
	}
	return out
}

func detail(s proposal.State) pages.ProposalDetailProps {
	p := pages.ProposalDetailProps{
		ID: s.ProposalID, Symbol: s.Symbol, Bucket: s.Bucket, Action: s.Action, OrderType: s.OrderType,
		Status: s.Status, StatusReason: s.StatusReason, Rationale: s.Rationale, ExitPlan: deref(s.ExitPlan),
	}
	p.Summary = []pages.ProposalField{
		{Label: "受付（JST）", Value: jst(s.ReceivedAt)},
		{Label: "最終更新（JST）", Value: jst(s.UpdatedAt)},
		{Label: "有効期限（JST）", Value: jst(s.ExpiresAt)},
		{Label: "取消の依頼（JST）", Value: optJST(s.CancelRequestedAt)},
		{Label: "数量（提案）", Value: optShares(s.Quantity)},
		{Label: "予算（提案）", Value: optYen(s.BudgetJPY)},
		{Label: "指値", Value: optYen(s.LimitPrice)},
		{Label: "数量（アプリが決めた数量）", Value: optShares(s.AppliedQuantity)},
	}
	r := s.Result
	p.Plan = []pages.ProposalField{
		{Label: "損切り価格（案）", Value: optYen(s.StopLossPrice)},
		{Label: "利確価格（案）", Value: optYen(s.TakeProfitPrice)},
		{Label: "実際に効いた損切り価格（案とアプリの遠い方）", Value: optYen(s.EffectiveStopLossPrice)},
		{Label: "決済の約定は損切りに届いたか", Value: reached(r.StopLossReached)},
		{Label: "決済の約定は利確に届いたか", Value: reached(r.TakeProfitReached)},
	}
	p.Loss = []pages.ProposalField{
		{Label: "あいりの見込み（案の最大損失）", Value: optYen(r.MaxLoss.ProposedJPY)},
		{Label: "アプリの見積り（数量 ×（約定見込み価格 − 損切り価格））", Value: optYen(r.MaxLoss.EstimatedJPY)},
		{Label: "実際の損失（決済済みのポジションの実現損失。利益なら 0）", Value: optYen(r.MaxLoss.ActualLossJPY)},
	}
	for _, src := range s.Sources {
		p.Sources = append(p.Sources, pages.ProposalSource{
			Title: src.Title, URL: src.URL, Href: linkHref(src.URL), RetrievedAt: jst(src.RetrievedAt),
		})
	}
	for _, e := range s.Events {
		p.Events = append(p.Events, pages.ProposalEvent{
			At: jst(e.At), From: deref(e.FromStatus), To: e.ToStatus, Reason: e.ReasonCode, Actor: e.Actor, Detail: detailText(e.Detail),
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
		risk := pages.ProposalField{Label: "Risk check", Value: passed(sg.RiskPassed)}
		reject := pages.ProposalField{Label: "拒否コード", Value: deref(sg.RejectCode)}
		if sg.RejectedAfterRisk { // passed Risk, refused by the application afterwards (issue #860)
			risk.Value, reject.Label = laterRejection, "理由コード"
		} else if end, ok := afterRiskEnd[status]; ok && !sg.RiskPassed { // passed Risk, no order came of it for another reason
			risk.Value, reject.Label = "Risk通過・"+end, "理由コード"
		}
		out = append(out, pages.ProposalLinked{Title: "トレードシグナル #" + strconv.FormatInt(sg.ID, 10), Fields: []pages.ProposalField{
			{Label: "時刻（JST）", Value: jst(sg.At)},
			{Label: "policy_version", Value: sg.PolicyVersion},
			risk, reject,
		}})
	}
	if o := r.Order; o != nil {
		out = append(out, pages.ProposalLinked{Title: "注文 #" + strconv.FormatInt(o.ID, 10), Fields: []pages.ProposalField{
			{Label: "売買 / 種別", Value: o.Side + " / " + o.OrderType},
			{Label: "状態", Value: o.Status},
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
		out = append(out, pages.ProposalLinked{Title: "ポジション #" + strconv.FormatInt(pos.ID, 10), Fields: []pages.ProposalField{
			{Label: "状態", Value: status},
			{Label: "数量", Value: shares(pos.Quantity)},
			{Label: "建値", Value: yen(pos.EntryPrice)},
			{Label: "建玉（JST）", Value: jst(pos.OpenedAt)},
			{Label: "決済（JST）", Value: optJST(pos.ClosedAt)},
			{Label: "決済価格", Value: optYen(pos.ExitPrice)},
			{Label: "決済理由", Value: deref(pos.ExitReason)},
			{Label: "保有時間", Value: minutes(pos.HoldingMinutes)},
			{Label: "手数料（買い・売り合計）", Value: optYen(pos.FeesJPY)},
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
const laterRejection = "Risk通過・後段で拒否"

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

// detailText is an event's detail object as the compact JSON text; null (no
// detail) is empty.
func detailText(raw []byte) string {
	if s := string(raw); s != "null" {
		return s
	}
	return ""
}
