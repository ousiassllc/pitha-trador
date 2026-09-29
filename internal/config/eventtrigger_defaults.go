package config

import "log/slog"

// Default FR-SCAN-1/FR-SCAN-2 event-trigger thresholds; each equals the
// shipped config/strategy.yaml scan.event_trigger value. They fill in keys
// an operator's older on-disk strategy.yaml lacks (see
// withEventTriggerDefaults).
const (
	DefaultReturn1mChangeThreshold           = 0.005
	DefaultVolumeRatioChangeThreshold        = 2.0
	DefaultSpreadChangeBpsThreshold          = 10.0
	DefaultOrderbookImbalanceChangeThreshold = 0.3
	DefaultTradeFlowImbalanceChangeThreshold = 0.4
)

// withEventTriggerDefaults fills the shipped default into every
// scan.event_trigger threshold that is unset (or not positive), warning so
// the operator can set it explicitly. eventtrigger.Detect fires on
// abs(change) >= threshold, so a threshold of 0 - the Go zero value of a
// key missing from a strategy.yaml predating it - would trigger FR-SCAN-1
// on every bar and defeat FR-SCAN-2's quiet-period suppression; a
// non-positive threshold is therefore never a meaningful setting.
func withEventTriggerDefaults(cfg *EventTriggerConfig) {
	fill := func(key string, dest *float64, def float64) {
		if *dest > 0 {
			return
		}
		slog.Warn("config: scan.event_trigger."+key+" is not set (or not positive) in strategy.yaml; using default",
			"default", def)
		*dest = def
	}
	fill("return_1m_change_threshold", &cfg.Return1mChangeThreshold, DefaultReturn1mChangeThreshold)
	fill("volume_ratio_change_threshold", &cfg.VolumeRatioChangeThreshold, DefaultVolumeRatioChangeThreshold)
	fill("spread_change_bps_threshold", &cfg.SpreadChangeBpsThreshold, DefaultSpreadChangeBpsThreshold)
	fill("orderbook_imbalance_change_threshold", &cfg.OrderbookImbalanceChangeThreshold, DefaultOrderbookImbalanceChangeThreshold)
	fill("trade_flow_imbalance_change_threshold", &cfg.TradeFlowImbalanceChangeThreshold, DefaultTradeFlowImbalanceChangeThreshold)
}
