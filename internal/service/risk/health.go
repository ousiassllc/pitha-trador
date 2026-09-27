package risk

import "context"

// HealthChecker reports whether the condition that triggered a
// market_data_down or jev_api_down Kill Switch has cleared, for
// AutoResume's FR-RISK-7 "市場データ復旧"/"Jev API復旧" classification.
// internal/service/marketdata and internal/service/jev (a later
// sub-scope's wiring) provide the real implementations;
// AlwaysHealthy is the placeholder default.
type HealthChecker interface {
	Healthy(ctx context.Context) (bool, error)
}

// AlwaysHealthy is the placeholder HealthChecker used until a real
// market-data/Jev-API health signal is wired in. Since nothing in this
// build yet calls Engine.TriggerKillSwitch with reason market_data_down
// or jev_api_down either, this default is inert either way: AutoResume
// only consults it for events that already exist.
type AlwaysHealthy struct{}

func (AlwaysHealthy) Healthy(context.Context) (bool, error) { return true, nil }
