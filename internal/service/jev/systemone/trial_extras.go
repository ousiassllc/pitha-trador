package systemone

import (
	"encoding/json"
	"math"
)

// trialExtras are the response fields only the trial path reads. They are
// decoded in a pass of their own, field by field, so that a field of an
// unexpected type is simply absent instead of failing the response: the
// answer set is what DecodeTrial validates, these are diagnostics.
type trialExtras struct {
	ID            string
	Provider      string
	costUSD       *float64
	probabilities map[string]map[string]float64
}

func decodeTrialExtras(body []byte) trialExtras {
	var raw struct {
		ID       json.RawMessage `json:"id"`
		Provider json.RawMessage `json:"provider"`
		Usage    struct {
			Cost json.RawMessage `json:"cost"`
		} `json:"usage"`
		Answers map[string]json.RawMessage `json:"answers"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		// The answer set already decoded; some other member has a shape
		// this pass does not model. Keep the diagnostics empty.
		return trialExtras{}
	}
	var e trialExtras
	_ = json.Unmarshal(raw.ID, &e.ID)
	_ = json.Unmarshal(raw.Provider, &e.Provider)
	// A pointer: JSON null decodes into a plain float64 as 0 without error.
	var cost *float64
	if err := json.Unmarshal(raw.Usage.Cost, &cost); err == nil && cost != nil && *cost >= 0 && !math.IsInf(*cost, 0) && !math.IsNaN(*cost) {
		e.costUSD = cost
	}
	for id, rawAnswer := range raw.Answers {
		var a struct {
			Probabilities map[string]float64 `json:"probabilities"`
		}
		if err := json.Unmarshal(rawAnswer, &a); err != nil {
			continue
		}
		if probs := a.Probabilities; len(probs) > 0 {
			if e.probabilities == nil {
				e.probabilities = make(map[string]map[string]float64)
			}
			e.probabilities[id] = probs
		}
	}
	return e
}
