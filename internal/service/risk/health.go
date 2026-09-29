package risk

import "context"

// HealthChecker reports whether the condition that triggered a
// market_data_down or jev_api_down Kill Switch has cleared, for
// AutoResume's FR-RISK-7 "市場データ復旧"/"Jev API復旧" classification, and
// (inverted) whether CheckMarketDataHealth/CheckJevAPIHealth should raise
// one. *marketdata.Client and *jev.Client implement it; AlwaysHealthy is
// the default when Config leaves it nil.
type HealthChecker interface {
	Healthy(ctx context.Context) (bool, error)
}

// AlwaysHealthy is the HealthChecker NewEngine uses for a nil
// Config.MarketDataHealth/JevAPIHealth, keeping that detector inert
// (internal/bootstrap injects the real ones).
type AlwaysHealthy struct{}

func (AlwaysHealthy) Healthy(context.Context) (bool, error) { return true, nil }
