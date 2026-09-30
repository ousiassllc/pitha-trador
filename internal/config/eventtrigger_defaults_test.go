package config_test

import (
	"os"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/featureengine/eventtrigger"
)

// A strategy.yaml predating scan.event_trigger (or one of its keys) must
// not leave a 0 threshold, which would make eventtrigger.Detect fire on
// every bar and defeat FR-SCAN-2.
func TestLoadStrategyBytes_FillsUnsetEventTriggerThresholds(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"no scan section", "fast_screener:\n  top_n: 20\n"},
		{"no event_trigger block", "scan:\n  full_scan_interval_seconds: 60\n"},
		{"empty event_trigger block", "scan:\n  event_trigger: {}\n"},
		{
			"only trade_flow key missing (pre-#201 file)",
			"scan:\n  event_trigger:\n" +
				"    return_1m_change_threshold: 0.005\n" +
				"    volume_ratio_change_threshold: 2.0\n" +
				"    spread_change_bps_threshold: 10\n" +
				"    orderbook_imbalance_change_threshold: 0.3\n",
		},
		{
			"explicit zeros and negatives",
			"scan:\n  event_trigger:\n    return_1m_change_threshold: 0\n    volume_ratio_change_threshold: -1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := config.LoadStrategyBytes([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("LoadStrategyBytes returned error: %v", err)
			}
			et := cfg.Scan.EventTrigger
			want := config.EventTriggerConfig{
				Return1mChangeThreshold:           config.DefaultReturn1mChangeThreshold,
				VolumeRatioChangeThreshold:        config.DefaultVolumeRatioChangeThreshold,
				SpreadChangeBpsThreshold:          config.DefaultSpreadChangeBpsThreshold,
				OrderbookImbalanceChangeThreshold: config.DefaultOrderbookImbalanceChangeThreshold,
				TradeFlowImbalanceChangeThreshold: config.DefaultTradeFlowImbalanceChangeThreshold,
			}
			if et != want {
				t.Errorf("Scan.EventTrigger = %+v, want %+v", et, want)
			}

			// End-to-end: a quiet bar must not trigger with the loaded thresholds.
			if quietSignal(et).Triggered() {
				t.Errorf("quiet bar Triggered() = true with loaded thresholds %+v, want false", et)
			}
		})
	}
}

func TestLoadStrategyBytes_KeepsConfiguredEventTriggerThresholds(t *testing.T) {
	cfg, err := config.LoadStrategyBytes([]byte("scan:\n  event_trigger:\n" +
		"    return_1m_change_threshold: 0.02\n" +
		"    trade_flow_imbalance_change_threshold: 0.9\n"))
	if err != nil {
		t.Fatalf("LoadStrategyBytes returned error: %v", err)
	}
	et := cfg.Scan.EventTrigger
	if et.Return1mChangeThreshold != 0.02 || et.TradeFlowImbalanceChangeThreshold != 0.9 {
		t.Errorf("configured thresholds overwritten: %+v", et)
	}
	if et.VolumeRatioChangeThreshold != config.DefaultVolumeRatioChangeThreshold {
		t.Errorf("VolumeRatioChangeThreshold = %v, want default %v", et.VolumeRatioChangeThreshold, config.DefaultVolumeRatioChangeThreshold)
	}
}

func TestLoadStrategy_FillsUnsetEventTriggerThresholdsFromFile(t *testing.T) {
	path := t.TempDir() + "/strategy.yaml"
	if err := os.WriteFile(path, []byte("scan:\n  full_scan_interval_seconds: 60\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	cfg, err := config.LoadStrategy(path)
	if err != nil {
		t.Fatalf("LoadStrategy returned error: %v", err)
	}
	if got := cfg.Scan.EventTrigger.TradeFlowImbalanceChangeThreshold; got != config.DefaultTradeFlowImbalanceChangeThreshold {
		t.Errorf("TradeFlowImbalanceChangeThreshold = %v, want default", got)
	}
}

// The Default* constants must track the shipped config/strategy.yaml.
func TestEventTriggerDefaults_MatchShippedStrategyYAML(t *testing.T) {
	cfg, err := config.LoadStrategy(repoPath(t, config.DefaultStrategyPath))
	if err != nil {
		t.Fatalf("LoadStrategy returned error: %v", err)
	}
	et := cfg.Scan.EventTrigger
	if et.Return1mChangeThreshold != config.DefaultReturn1mChangeThreshold ||
		et.VolumeRatioChangeThreshold != config.DefaultVolumeRatioChangeThreshold ||
		et.SpreadChangeBpsThreshold != config.DefaultSpreadChangeBpsThreshold ||
		et.OrderbookImbalanceChangeThreshold != config.DefaultOrderbookImbalanceChangeThreshold ||
		et.TradeFlowImbalanceChangeThreshold != config.DefaultTradeFlowImbalanceChangeThreshold {
		t.Errorf("config/strategy.yaml scan.event_trigger %+v diverges from Default* constants", et)
	}
}

func fptr(v float64) *float64 { return &v }

// quietSignal runs eventtrigger.Detect over a quiet two-bar pair (tiny moves on
// every continuous signal) using the thresholds as config loaded them.
func quietSignal(et config.EventTriggerConfig) eventtrigger.Signal {
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	prev := domain.Snapshot{
		Timestamp: now.Add(-time.Minute), Price: 2000, SpreadBps: fptr(5),
		Feature: domain.Feature{
			Return1m: fptr(0.0001), VolumeRatio5m: fptr(1.0), PriceVsVWAPBps: 10,
			OrderbookImbalance: fptr(0.10), TradeFlowImbalance: fptr(0.10),
		},
	}
	curr := domain.Snapshot{
		Timestamp: now, Price: 2001, SpreadBps: fptr(5),
		Feature: domain.Feature{
			Return1m: fptr(0.0002), VolumeRatio5m: fptr(1.0), PriceVsVWAPBps: 10,
			OrderbookImbalance: fptr(0.10), TradeFlowImbalance: fptr(0.11),
		},
	}
	th := eventtrigger.Thresholds{
		Return1mChange:           et.Return1mChangeThreshold,
		VolumeRatioChange:        et.VolumeRatioChangeThreshold,
		SpreadChangeBps:          et.SpreadChangeBpsThreshold,
		OrderbookImbalanceChange: et.OrderbookImbalanceChangeThreshold,
		TradeFlowImbalanceChange: et.TradeFlowImbalanceChangeThreshold,
	}
	return eventtrigger.Detect(prev, curr, []domain.Snapshot{{Price: 1990}, {Price: 2010}}, th, false)
}
