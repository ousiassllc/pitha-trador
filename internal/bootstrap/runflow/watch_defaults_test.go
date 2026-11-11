package runflow_test

import (
	"path/filepath"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/rankingwatch"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/infolimit"
	"github.com/ousiassllc/pitha-trador/internal/service/marketdata/kabu"
)

// issue #1055: the defaults stay today's behaviour (kabu 45 = 50 slots - 5 kept
// for REST, 立花 120, the information API at 8 requests/second with the official
// 10 as the hard ceiling, full scan off, top_n 20).
func TestDefaultsArePinned(t *testing.T) {
	if kabu.MaxStreamSymbols != 45 || rankingwatch.DefaultMaxWatched != 45 || watchsettings.MaxKabuWatched != 45 || watchsettings.DefaultMaxWatched != 45 {
		t.Errorf("kabu cap %d / DefaultMaxWatched %d / watchsettings %d/%d, want all 45",
			kabu.MaxStreamSymbols, rankingwatch.DefaultMaxWatched, watchsettings.MaxKabuWatched, watchsettings.DefaultMaxWatched)
	}
	if got := (&kabu.Adapter{}).Capabilities(); got.MaxStreamSymbols != 45 {
		t.Errorf("kabu capabilities = %+v, want 45 symbols", got)
	}
	if adapter.MaxStreamSymbols != 120 || tachibanasource.MaxTachibanaWatchSymbols != 120 || tachibanasource.TachibanaScreenTopNMax != 120 {
		t.Errorf("立花 cap %d / %d / %d, want 120", adapter.MaxStreamSymbols, tachibanasource.MaxTachibanaWatchSymbols, tachibanasource.TachibanaScreenTopNMax)
	}
	if infolimit.DefaultMaxPerSecond != 8 || infolimit.OfficialMaxPerSecond != 10 ||
		config.DefaultKabuInfoAPIMaxPerSecond != 8 || config.OfficialKabuInfoAPIMaxPerSecond != 10 {
		t.Error("the information API limit is no longer 8 (default) / 10 (official)")
	}

	state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Cleanup(func() { _ = state.Close() })
	if state.Strategy.Scan.FullScanOn() {
		t.Error("scan.full_scan_enabled is on by default")
	}
	if state.Strategy.FastScreener.TopN != 20 {
		t.Errorf("screener.top_n = %d, want 20", state.Strategy.FastScreener.TopN)
	}
	if state.Strategy.Scan.KabuInfoAPIMaxPerSecond != 8 {
		t.Errorf("scan.kabu_info_api_max_per_second = %d, want 8", state.Strategy.Scan.KabuInfoAPIMaxPerSecond)
	}
}
