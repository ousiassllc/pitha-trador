package datasetexport

import (
	"strings"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// kind is how a SQL-read column is validated and written.
type kind int

const (
	kindNum  kind = iota // a number; anything else the column holds is written as empty
	kindEnum             // one of field.enum; anything else is written as empty
	kindText             // an identifier-like text (a sector name)
)

// field is one column read by the decisions query. Its SQL is generated from the constants
// below only (a fixed whitelist, no caller text), and wraps the column so a value of an
// unexpected type or outside its enum comes out NULL instead of reaching a file.
type field struct {
	name string
	// expr is the table-qualified column (d. = jev_decisions, m. = market_snapshots, i. =
	// instruments); json is the key of d.response_json instead (json_extract, fixed keys only).
	expr, json string
	kind       kind
	enum       []string
	// source, unit and meaning are the documentation of the column (docs/architecture/research-tools/training-dataset.md).
	source, unit, meaning string
}

func (f field) selectSQL() string {
	e := f.expr
	if f.json != "" {
		e = "json_extract(d.response_json, '$." + f.json + "')"
	}
	var guard string
	switch f.kind {
	case kindNum:
		if f.json != "" {
			guard = "json_type(d.response_json, '$." + f.json + "') IN ('integer', 'real')"
		} else {
			guard = "typeof(" + e + ") IN ('integer', 'real')"
		}
	case kindEnum:
		quoted := make([]string, len(f.enum))
		for i, v := range f.enum {
			quoted[i] = "'" + v + "'"
		}
		guard = e + " IN (" + strings.Join(quoted, ", ") + ")"
	default:
		guard = "typeof(" + e + ") = 'text'"
	}
	out := "CASE WHEN " + guard + " THEN " + e + " END"
	if f.json != "" {
		out = "CASE WHEN json_valid(d.response_json) THEN " + out + " END" // json_extract of a malformed body is an error
	}
	return out
}

func num(name, expr, source, unit, meaning string) field {
	return field{name: name, expr: expr, kind: kindNum, source: source, unit: unit, meaning: meaning}
}

func enum(name, expr, source string, values []string, meaning string) field {
	return field{name: name, expr: expr, kind: kindEnum, enum: values, source: source, meaning: meaning}
}

func answerNum(key, meaning string) field {
	return field{name: "answer_" + key, json: key, kind: kindNum, source: "jev_decisions.response_json $." + key, unit: "0〜1", meaning: meaning}
}

var (
	directions    = []string{domain.JevDirectionLong, domain.JevDirectionShort, domain.JevDirectionNone}
	answerRegimes = []string{domain.JevRegimeTrend, domain.JevRegimeRange, domain.JevRegimeBreakout, domain.JevRegimeChaotic}
	entryQualitys = []string{domain.JevEntryQualityPoor, domain.JevEntryQualityFair, domain.JevEntryQualityGood, domain.JevEntryQualityStrong, domain.JevEntryQualityExceptional}
	barrierHits   = []string{"tp", "sl", "timeout"}
)

// decisionFields come from the jev_decisions row: Jev's structured answer (fixed fields only,
// never response_json / state_json themselves) and the values the decision recorded.
var decisionFields = []field{
	enum("answer_direction", "d.direction", "jev_decisions.direction", directions, "Jev の答え: 方向（LONG / SHORT / NONE）"),
	num("answer_confidence", "d.confidence", "jev_decisions.confidence", "0〜1", "Jev の答え: 方向への確信度（検証済みの確率ではない）"),
	{name: "answer_regime", json: "regime", kind: kindEnum, enum: answerRegimes, source: "jev_decisions.response_json $.regime", meaning: "Jev の答え: 相場の型（TREND / RANGE / BREAKOUT / CHAOTIC）"},
	{name: "answer_entry_quality", json: "entry_quality", kind: kindEnum, enum: entryQualitys, source: "jev_decisions.response_json $.entry_quality", meaning: "Jev の答え: エントリーの質（poor〜exceptional）"},
	answerNum("toxic_flow", "Jev の答え: 有害な約定の流れである確率"),
	answerNum("liquidity_stressed", "Jev の答え: 流動性が逼迫している確率"),
	answerNum("continuation_probability", "Jev の答え: 動きが続く確率"),
	num("data_age_ms", "d.data_age_ms", "jev_decisions.data_age_ms", "ミリ秒", "判断時点の市場データの古さ"),
	enum("market_regime", "d.market_regime", "jev_decisions.market_regime", []string{"trend", "mean_revert", "unknown"}, "判断時点の相場状態（市場指数から判定）"),
	enum("ab_arm", "d.ab_arm", "jev_decisions.ab_arm", []string{"momentum", "regime"}, "判断した A/B アーム"),
	num("regime_volatility_bps", "d.regime_volatility_bps", "jev_decisions.regime_volatility_bps", "bps", "相場状態の判定に使った市場指数のボラティリティ"),
	num("regime_autocorr", "d.regime_autocorr", "jev_decisions.regime_autocorr", "-1〜1", "相場状態の判定に使った自己相関"),
	num("vwap_z", "d.vwap_z", "jev_decisions.vwap_z", "z スコア", "平均回帰レジームでの VWAP 乖離の z スコア"),
	num("ofi_short", "d.ofi_short", "jev_decisions.ofi_short", "株", "判断時点の Order Flow Imbalance（短い窓。買い圧力が正）"),
	num("ofi_mid", "d.ofi_mid", "jev_decisions.ofi_mid", "株", "同（中の窓）"),
	num("ofi_long", "d.ofi_long", "jev_decisions.ofi_long", "株", "同（長い窓）"),
	num("ofi_window_short_seconds", "d.ofi_window_short_seconds", "jev_decisions.ofi_window_short_seconds", "秒", "ofi_short の窓の幅"),
	num("ofi_window_mid_seconds", "d.ofi_window_mid_seconds", "jev_decisions.ofi_window_mid_seconds", "秒", "ofi_mid の窓の幅"),
	num("ofi_window_long_seconds", "d.ofi_window_long_seconds", "jev_decisions.ofi_window_long_seconds", "秒", "ofi_long の窓の幅"),
	num("microprice_dev_bps", "d.microprice_dev_bps", "jev_decisions.microprice_dev_bps", "bps", "マイクロプライスの中値からの乖離（板の生の値は出さない）"),
}

func bar(name, unit, meaning string) field {
	return num(name, "m."+name, "market_snapshots."+name, unit, meaning)
}

// barFields are the derived feature columns of the decision's own 1-minute bar
// (market_snapshots at the decision's instrument_id and timestamp). Raw board and quote data
// (bid, ask, bid_depth, ask_depth, microprice, raw_data_json) and the price, VWAP, volume and
// turnover levels are not among them.
var barFields = []field{
	bar("spread_bps", "bps", "気配のスプレッド"),
	bar("return_1m", "比（0.01=+1%）", "1 分リターン"),
	bar("return_3m", "比", "3 分リターン"),
	bar("return_5m", "比", "5 分リターン"),
	bar("return_15m", "比", "15 分リターン"),
	bar("return_30m", "比", "30 分リターン"),
	bar("price_vs_vwap_bps", "bps", "VWAP からの乖離"),
	bar("vwap_slope", "比", "VWAP の 5 分変化率"),
	bar("vwap_cross_direction", "-1 / 0 / 1", "前の足から VWAP を上抜け（1）・下抜け（-1）・なし（0）"),
	bar("volume_ratio_1m", "倍", "1 分出来高の平常比"),
	bar("volume_ratio_5m", "倍", "5 分出来高の平常比"),
	bar("high_distance_5m", "比", "直近 5 分高値からの距離（0 以下）"),
	bar("low_distance_5m", "比", "直近 5 分安値からの距離（0 以上）"),
	bar("session_high_distance", "比", "当日高値からの距離"),
	bar("session_low_distance", "比", "当日安値からの距離"),
	bar("atr_1m", "円", "1 分 ATR"),
	bar("atr_5m", "円", "5 分 ATR"),
	bar("realized_vol_5m", "比", "5 分の実現ボラティリティ"),
	bar("realized_vol_15m", "比", "15 分の実現ボラティリティ"),
	bar("volatility_expansion_ratio", "倍", "ボラティリティの拡大率"),
	bar("orderbook_imbalance", "-1〜1", "板の数量の偏り（板の生の数量は出さない）"),
	bar("buy_trade_ratio", "0〜1", "直近 5 分の買い約定の割合"),
	bar("sell_trade_ratio", "0〜1", "直近 5 分の売り約定の割合"),
	bar("trade_flow_imbalance", "-1〜1", "買い約定と売り約定の偏り"),
	bar("market_return_1m", "比", "市場指数の 1 分リターン"),
	bar("market_return_5m", "比", "市場指数の 5 分リターン"),
	bar("sector_return_5m", "比", "同業種指数の 5 分リターン"),
	bar("stock_vs_sector_relative_strength", "比", "return_5m − sector_return_5m"),
	bar("market_breadth", "-1〜1", "値上がり・値下がり銘柄数の偏り"),
	bar("volume_1m", "株", "直近 1 分の出来高"),
	bar("volume_5m", "株", "直近 5 分の出来高"),
	bar("turnover_1m", "円", "直近 1 分の売買代金"),
	bar("turnover_5m", "円", "直近 5 分の売買代金"),
	bar("special_quote", "0 / 1", "特別気配か"),
	bar("lendable", "0 / 1", "貸借銘柄か（空は不明）"),
	enum("price_limit", "m.price_limit", "market_snapshots.price_limit", []string{"up", "down"}, "ストップ高（up）・ストップ安（down）。空は該当なし"),
}

// instrumentFields come from the instrument master.
var instrumentFields = []field{
	{name: "sector", expr: "i.sector", kind: kindText, source: "instruments.sector", meaning: "業種（銘柄マスタ）"},
	enum("asset_class", "i.asset_class", "instruments.asset_class", []string{"stock", "bond_etf"}, "資産クラス"),
}
