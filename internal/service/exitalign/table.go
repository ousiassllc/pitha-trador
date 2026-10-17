package exitalign

import (
	"fmt"
	"math"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Provisional tolerances of the mismatch detector. They are not derived from
// data (there are no paper trades yet; see FR-CAL-4's 決定記録): a label basis
// and an exit within them are treated as "the same definition". Review them
// together with the cost assumption once the paper evidence exists
// (docs/requirements/functional/components-pipeline.md FR-CAL-4).
const (
	// ToleranceBps is how far the label's gross TP/SL level may be from the
	// actual exit's before they count as different.
	ToleranceBps = 5.0
	// ToleranceMinutes is the same for the label timeout versus the maximum
	// holding time.
	ToleranceMinutes = 1.0
	// MaxCostAllowanceBps bounds how much of the label cost may be something
	// the fill model does not charge (the spread, which the fills take from
	// the board): label cost - fill round-trip cost must lie in
	// [0, MaxCostAllowanceBps].
	MaxCostAllowanceBps = 15.0
)

// Warning codes of AlignmentWarning.Code.
const (
	WarnTakeProfit = "take_profit_mismatch"
	WarnStopLoss   = "stop_loss_mismatch"
	WarnHorizon    = "horizon_mismatch"
	WarnCost       = "cost_mismatch"
	WarnCostSides  = "cost_direction_asymmetry"
)

// ExitConfig is the actual exit rule set the Paper Execution Engine applies
// (execution.Config's FR-EXIT-2 fields); a zero value disables that condition.
// The conditions market_snapshots has no data for (Jev方向反転,
// continuation_probability低下, 引け前強制決済) are not part of it: the replay
// cannot evaluate them (NotEvaluatedExits).
type ExitConfig struct {
	StopLossPct       float64
	TakeProfitPct     float64
	TrailingStopPct   float64
	MaxHoldingMinutes int
}

func (e ExitConfig) maxHolding() time.Duration {
	return time.Duration(e.MaxHoldingMinutes) * time.Minute
}

const bpsPerPct = 100.0

// Correspondence maps the label basis (b, with the cost settings moving its
// cost) onto the actual exit and fill-model cost and returns the table and its
// mismatch warnings.
//
// The label's TP/SL widths are net of the cost, so they are compared at the
// gross price move that touches them: TP + cost for the take-profit, SL - cost
// for the stop-loss (barrier.go firstBarrier). The exit's percentages are gross
// price moves already. The timeout maps to the maximum holding time. The
// label's cost (all-in: fees, spread, slippage) maps to the fill model's
// modelled round trip (fees + slippage; the spread is taken from the board).
// The trailing stop has no label counterpart and is listed for the record.
func Correspondence(b barriersettings.Barriers, exit ExitConfig, costs costsettings.Settings) ([]AlignmentRow, []AlignmentWarning) {
	long, short := domain.JevDirectionLong, domain.JevDirectionShort
	labelCost := costs.LabelCostBps(b.CostBps, long)
	labelCostShort := costs.LabelCostBps(b.CostBps, short)

	tpLabel, tpExit := b.TakeProfitBps+labelCost, exit.TakeProfitPct*bpsPerPct
	slLabel, slExit := b.StopLossBps-labelCost, exit.StopLossPct*bpsPerPct
	horizonLabel, horizonExit := float64(b.TimeoutMinutes()), float64(exit.MaxHoldingMinutes)
	fillCost, fillCostShort := costs.RoundTripBps(long), costs.RoundTripBps(short)

	tpOK := exit.TakeProfitPct > 0 && within(tpLabel, tpExit, ToleranceBps)
	slOK := exit.StopLossPct > 0 && within(slLabel, slExit, ToleranceBps)
	horizonOK := exit.MaxHoldingMinutes > 0 && within(horizonLabel, horizonExit, ToleranceMinutes)
	costOK := allowanceOK(labelCost, fillCost) && allowanceOK(labelCostShort, fillCostShort)

	rows := []AlignmentRow{
		{Key: "take_profit", Item: "利確", Comparable: true, Aligned: tpOK,
			LabelText: fmt.Sprintf("TP %s + コスト %s = グロス +%s bps", bps(b.TakeProfitBps), bps(labelCost), bps(tpLabel)),
			ExitText:  pctText("+", exit.TakeProfitPct, exit.TakeProfitPct*bpsPerPct)},
		{Key: "stop_loss", Item: "損切り", Comparable: true, Aligned: slOK,
			LabelText: fmt.Sprintf("SL %s - コスト %s = グロス -%s bps", bps(b.StopLossBps), bps(labelCost), bps(slLabel)),
			ExitText:  pctText("-", exit.StopLossPct, exit.StopLossPct*bpsPerPct)},
		{Key: "horizon", Item: "保有時間", Comparable: true, Aligned: horizonOK,
			LabelText: fmt.Sprintf("タイムアウト %d 分", b.TimeoutMinutes()),
			ExitText:  minutesText(exit.MaxHoldingMinutes)},
		{Key: "cost", Item: "コスト（往復）", Comparable: true, Aligned: costOK,
			LabelText: fmt.Sprintf("%s bps（手数料・スプレッド・スリッページ込み）", bps(labelCost)),
			ExitText:  fmt.Sprintf("%s bps（約定モデル: 手数料＋スリッページ。スプレッドは板から別途）", bps(fillCost))},
		{Key: "trailing_stop", Item: "トレーリングストップ", Comparable: false, Aligned: exit.TrailingStopPct == 0,
			LabelText: "表現できない（ラベルはバリア 2 本とタイムアウトのみ）",
			ExitText:  pctText("", exit.TrailingStopPct, 0)},
	}

	var warnings []AlignmentWarning
	add := func(ok bool, code, format string, args ...any) {
		if !ok {
			warnings = append(warnings, AlignmentWarning{Code: code, Message: fmt.Sprintf(format, args...)})
		}
	}
	add(tpOK, WarnTakeProfit, "ラベルの利確（グロス +%s bps）と実出口の利確（%s）が一致しません（許容 %g bps）。メタモデルの勝ち幅は、実際に取る取引の勝ち幅と別物の可能性があります。",
		bps(tpLabel), disabledOr(exit.TakeProfitPct > 0, "+"+bps(tpExit)+" bps"), ToleranceBps)
	add(slOK, WarnStopLoss, "ラベルの損切り（グロス -%s bps）と実出口の損切り（%s）が一致しません（許容 %g bps）。メタモデルの負け幅は、実際に取る取引の負け幅と別物の可能性があります。",
		bps(slLabel), disabledOr(exit.StopLossPct > 0, "-"+bps(slExit)+" bps"), ToleranceBps)
	add(horizonOK, WarnHorizon, "ラベルのタイムアウト（%d 分）と実出口の最大保有時間（%s）が一致しません（許容 %g 分）。",
		b.TimeoutMinutes(), disabledOr(exit.MaxHoldingMinutes > 0, minutesText(exit.MaxHoldingMinutes)), ToleranceMinutes)
	add(costOK, WarnCost, "ラベルのコスト（LONG %s / SHORT %s bps）が約定モデルの往復コスト（LONG %s / SHORT %s bps、スプレッド除く）と整合しません（差は 0〜%g bps の範囲であるべきです）。",
		bps(labelCost), bps(labelCostShort), bps(fillCost), bps(fillCostShort), MaxCostAllowanceBps)
	add(math.Abs(labelCostShort-labelCost) <= ToleranceBps, WarnCostSides, "ラベルのコストが LONG（%s bps）と SHORT（%s bps）で %g bps を超えて異なります。上の表は LONG の値です。",
		bps(labelCost), bps(labelCostShort), ToleranceBps)
	return rows, warnings
}

func within(a, b, tolerance float64) bool { return math.Abs(a-b) <= tolerance }

// allowanceOK reports whether the part of the label cost the fill model does
// not charge (the spread allowance) is in [0, MaxCostAllowanceBps].
func allowanceOK(labelCost, fillCost float64) bool {
	allowance := labelCost - fillCost
	return allowance >= 0 && allowance <= MaxCostAllowanceBps
}

func bps(v float64) string { return fmt.Sprintf("%g", math.Round(v*100)/100) }

func disabledOr(enabled bool, text string) string {
	if !enabled {
		return "無効"
	}
	return text
}

func pctText(sign string, pct, bpsValue float64) string {
	if pct <= 0 {
		return "無効"
	}
	if bpsValue == 0 {
		return fmt.Sprintf("%g %%", pct)
	}
	return fmt.Sprintf("%s%g %%（%s%s bps）", sign, pct, sign, bps(bpsValue))
}

func minutesText(minutes int) string {
	if minutes <= 0 {
		return "無効"
	}
	return fmt.Sprintf("最大保有 %d 分", minutes)
}
