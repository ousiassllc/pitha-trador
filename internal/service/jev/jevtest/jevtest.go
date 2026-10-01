// Package jevtest provides httptest handlers that answer jev.Client calls
// in the TypeSafe AI /v1/systemone wire format, so tests of Scout, Trader
// and their callers can script a domain-level ScoutResponse /
// TraderResponse without hand-writing wire JSON.
package jevtest

import (
	"encoding/json"
	"net/http"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// ModelOrDefault is the model reported when a response has no ModelID.
const ModelOrDefault = "jev-test"

// ScoutHandler answers every request with resp encoded as a systemone
// response. An empty MomentumQuality is answered as "weak" so a test that
// does not care about it still gets a valid response.
func ScoutHandler(resp jev.ScoutResponse) http.HandlerFunc {
	return write(modelID(resp.ModelID), map[string]any{
		"interesting_now":   Noul(resp.InterestingNow),
		"momentum_quality":  Choice(orDefault(resp.MomentumQuality, jev.MomentumQualityWeak), 0.8),
		"liquidity_ok":      Noul(resp.LiquidityOk),
		"abnormal_activity": Noul(resp.AbnormalActivity),
	})
}

// TraderHandler answers every request with resp encoded as a systemone
// response; resp.Confidence becomes the direction answer's confidence.
// Empty Direction/Regime/EntryQuality are answered as NONE/RANGE/poor.
func TraderHandler(resp jev.TraderResponse) http.HandlerFunc {
	return write(modelID(resp.ModelID), map[string]any{
		"direction":                Choice(orDefault(resp.Direction, domain.JevDirectionNone), resp.Confidence),
		"regime":                   Choice(orDefault(resp.Regime, domain.JevRegimeRange), 0.8),
		"entry_quality":            Choice(orDefault(resp.EntryQuality, domain.JevEntryQualityPoor), 0.8),
		"toxic_flow":               Noul(resp.ToxicFlow),
		"liquidity_stressed":       Noul(resp.LiquidityStressed),
		"continuation_probability": Noul(resp.ContinuationProbability),
	})
}

// Noul returns a wire noul answer.
func Noul(v float64) map[string]any {
	return map[string]any{"type": "noul", "noul": v}
}

// Choice returns a wire choice answer for value with the given
// confidence.
func Choice(value string, confidence float64) map[string]any {
	return map[string]any{
		"type":          "choice",
		"choice":        value,
		"probabilities": map[string]float64{value: confidence},
		"confidence":    confidence,
	}
}

// Response encodes a systemone response body with the given model and
// answers.
func Response(model string, answers map[string]any) []byte {
	body, err := json.Marshal(map[string]any{
		"model":   model,
		"answers": answers,
		"usage":   map[string]int{"input_tokens": 1200, "output_tokens": 40},
	})
	if err != nil {
		panic(err) // answers are plain maps of marshalable values
	}
	return body
}

func write(model string, answers map[string]any) http.HandlerFunc {
	body := Response(model, answers)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}
}

func modelID(id string) string { return orDefault(id, ModelOrDefault) }

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
