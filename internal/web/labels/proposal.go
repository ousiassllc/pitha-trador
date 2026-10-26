package labels

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ProposalReasons names the codes an order proposal's status_reason, its
// events and its signal's reject code carry: the processor's own codes
// (internal/bootstrap/proposalexec Reason*), the Risk Engine's
// (internal/service/risk/reason, correlation, sizing, bucket Reason*) and the
// daily entry halts' (domain.DailyHaltReason*).
var ProposalReasons = map[string]string{
	// the processor's
	"expired":                    "期限切れ",
	"cancel_requested":           "取消の依頼",
	"unknown_symbol":             "銘柄が見つからない",
	"inactive_symbol":            "銘柄が無効",
	"bucket_mismatch":            "枠が銘柄と合わない",
	"outside_trading_session":    "立会時間外",
	"stale_quote":                "気配が古い",
	"size_zero":                  "数量が 0 株になる",
	"max_trade_loss":             "1回の最大損失を超える",
	"quantity_reduced":           "数量を減らした",
	"position_already_open":      "すでにポジションがある",
	"pending_order_exists":       "未約定の注文がある",
	"symbol_in_cooldown":         "損失後の待機中の銘柄",
	"no_open_position":           "決済するポジションがない",
	"order_rejected":             "注文が拒否された",
	"order_cancelled":            "注文が取り消された",
	"processing_error":           "処理中にエラーが出た",
	"recovered_after_restart":    "再起動後に復旧した",
	"position_not_from_proposal": "注文提案以外で建てたポジション",
	"stop_loss_not_below_price":  "損切り価格が約定見込み価格以上",
	// the Risk Engine's
	"system_paused":                "システムが一時停止中",
	"kill_switch_active":           "Kill Switch が発動中",
	"cooldown_after_loss":          "損失後の待機中",
	"max_open_positions":           "同時に持てるポジション数の上限",
	"max_daily_loss_pct":           "日次損失率の上限",
	"max_consecutive_losses":       "連続損失回数の上限",
	"max_spread_bps":               "スプレッドの上限",
	"risk_engine_error":            "リスク判定でエラー（安全のため拒否）",
	"snapshots_unavailable":        "市況データを読めない",
	"max_same_direction_positions": "同じ向きのポジション数の上限",
	"market_adverse_to_direction":  "市場全体が売買の向きに不利",
	"max_trade_loss_pct":           "1回の最大損失率の上限",
	"max_position_per_symbol_pct":  "1銘柄の建玉上限",
	"max_total_exposure_pct":       "総エクスポージャの上限",
	"invalid_sizing_input":         "サイズ計算の入力が不正",
	"bucket_disabled":              "この枠は無効",
	"bucket_budget_exceeded":       "枠の予算を超える",
	// the daily entry halts'
	"daily_loss_limit_jpy": "1日の損失上限",
	"profit_lock":          "利益確保",
	"max_daily_trades":     "1日の最大取引回数",
	// a signal row with no recorded reason
	"unknown": "理由の記録なし",
}

// ProposalReason is the name of an order proposal's reason code. A Kill Switch
// reason (a Risk Engine reject that raised one) reads as in the Kill Switch
// history.
func ProposalReason(code string) string {
	if name, ok := ProposalReasons[code]; ok {
		return name
	}
	if name, ok := KillReasons[code]; ok {
		return name
	}
	return lookup("proposal_reason", ProposalReasons, code, Unknown)
}

// Actors names order_proposal_events.actor (orderproposal.Actor*).
var Actors = map[string]string{"mcp": "AI（MCP 経由）", "app": "アプリ", "gui": "画面の操作"}

// Actor is the name of who made an order proposal event.
func Actor(code string) string { return lookup("actor", Actors, code, Other) }

// eventDetailKeys names the keys of an order proposal event's detail object
// (what internal/bootstrap/proposalexec and proposalstore write).
var eventDetailKeys = map[string]string{
	"latest_price":            "直近の価格",
	"expected_entry_price":    "約定見込み価格",
	"stop_loss_price":         "損切り価格",
	"requested_quantity":      "提案の数量",
	"applied_quantity":        "適用した数量",
	"limited_by":              "数量を減らした理由",
	"recovered_after_restart": "再起動後に復旧",
	"trade_signal_id":         "トレードシグナル",
}

// eventDetailValues names the coded values of those keys.
var eventDetailValues = map[string]map[string]string{
	"limited_by": {"position_size": "ポジションサイズの上限"},
}

// EventDetail is an order proposal event's detail object in words: the known
// keys by name, in a fixed order, and a count of the ones this screen has no
// name for (they stay in the API and the MCP). "" for no detail.
func EventDetail(raw []byte) string {
	var fields map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &fields) != nil || len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	other := 0
	for k := range fields {
		if _, ok := eventDetailKeys[k]; ok {
			keys = append(keys, k)
		} else {
			other++
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		parts = append(parts, eventDetailKeys[k]+": "+eventDetailValue(k, fields[k]))
	}
	if other > 0 {
		parts = append(parts, fmt.Sprintf("ほか %d 項目", other))
	}
	return strings.Join(parts, "・")
}

func eventDetailValue(key string, v any) string {
	switch x := v.(type) {
	case bool:
		if x {
			return "はい"
		}
		return "いいえ"
	case float64:
		if key == "trade_signal_id" {
			return strconv.FormatFloat(x, 'f', 0, 64) + " 番"
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		if names, ok := eventDetailValues[key]; ok {
			if name, ok := names[x]; ok {
				return name
			}
		}
		return Unknown
	}
	return Unknown
}
