package adoptionview

import (
	"database/sql"
	"encoding/json"
	"time"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
	"github.com/ousiassllc/pitha-trador/internal/service/changeguard"
)

// Stored are the metalabel_models values the view is derived from, as stored.
type Stored struct {
	OOSSamples, OOSTakes, OOSDays  int64
	EVLow, EVHigh, AUCLow, AUCHigh sql.NullFloat64
	// AdoptionJSON and ForwardJSON are the adoption_json / forward_json columns.
	AdoptionJSON, ForwardJSON sql.NullString
}

// Build derives the view of one stored model.
func Build(s Stored) View {
	v := View{OOSEVCI: interval(s.EVLow, s.EVHigh), OOSAUCCI: interval(s.AUCLow, s.AUCHigh)}
	adoption, status := readAdoption(s.AdoptionJSON)
	v.AdoptionRecordedStatus = status

	var criteria *Criteria
	if status == StatusRecorded {
		c := adoption.Criteria
		criteria = &Criteria{MinOOSSamples: c.MinOOSSamples, MinTakes: c.MinTakes, MinOOSDays: c.MinOOSDays,
			MinEVLowerBps: c.MinEVLowerBps, MinAUCLower: c.MinAUCLower, Alpha: c.Alpha, Resamples: c.Resamples}
		v.Criteria, v.OOSTakes, v.OOSDays = criteria, &s.OOSTakes, &s.OOSDays
		v.Comparison, v.Gate = comparison(adoption.Comparison), gate(adoption.Gate)
	}
	v.NumericChecks = checks(s, criteria, v)
	v.FirstUnmet, v.NumericComplete = v.NumericChecks.firstUnmet()
	if v.OOSTakes != nil && s.OOSSamples > 0 {
		rate := float64(s.OOSTakes) / float64(s.OOSSamples)
		v.TakeRate = &rate
	}

	switch {
	case status != StatusRecorded:
		v.ComparisonStatus = ComparisonNotRecorded
	case v.Comparison != nil:
		v.ComparisonStatus = ComparisonRecorded
	default:
		v.ComparisonStatus = ComparisonNotPerformed
	}
	v.Forward, v.ForwardStatus = forward(s.ForwardJSON, status)
	return v
}

// readAdoption decodes adoption_json. The criteria are recorded when
// min_oos_samples is positive, the Calibration API's own rule (a row stored
// before issue #780 holds "{}", which decodes to zero criteria).
func readAdoption(raw sql.NullString) (repo.Adoption, string) {
	if !raw.Valid {
		return repo.Adoption{}, StatusNotRecorded
	}
	var a repo.Adoption
	if err := json.Unmarshal([]byte(raw.String), &a); err != nil {
		return repo.Adoption{}, StatusUnreadable
	}
	if a.Criteria.MinOOSSamples <= 0 {
		return repo.Adoption{}, StatusNotRecorded
	}
	return a, StatusRecorded
}

func interval(low, high sql.NullFloat64) *Interval {
	if !low.Valid || !high.Valid {
		return nil
	}
	return &Interval{Low: low.Float64, High: high.Float64}
}

func intervalOf(iv *repo.Interval) *Interval {
	if iv == nil {
		return nil
	}
	return &Interval{Low: iv.Low, High: iv.High}
}

func comparison(c *repo.Comparison) *Comparison {
	if c == nil {
		return nil
	}
	return &Comparison{ActiveVersion: c.ActiveVersion, Comparable: c.Comparable, PairedSamples: c.PairedSamples,
		CandidateBps: c.CandidateBps, ActiveBps: c.ActiveBps, DiffBps: c.DiffBps, DiffCI: intervalOf(c.DiffCI), ActiveEVCI: intervalOf(c.ActiveEVCI)}
}

// gate returns the gate with a fixed reason code; the stored detail is dropped.
func gate(g *repo.Gate) *Gate {
	if g == nil {
		return nil
	}
	reason := g.Reason
	if reason != changeguard.ReasonModelSwapRateLimited && reason != changeguard.ReasonPolicyChangeFreeze {
		reason = ReasonOther
	}
	return &Gate{Reason: reason, Until: rfc3339(g.Until)}
}

func forward(raw sql.NullString, adoption string) (*Forward, string) {
	var f repo.Forward
	if !raw.Valid || json.Unmarshal([]byte(raw.String), &f) != nil {
		return nil, ForwardNotRecorded
	}
	if f.EvaluatedAt == "" {
		if adoption != StatusRecorded {
			return nil, ForwardNotRecorded
		}
		return nil, ForwardNotEvaluated
	}
	return &Forward{EvaluatedAt: rfc3339(f.EvaluatedAt), From: rfc3339(f.From), Samples: f.Samples, Takes: f.Takes, Days: f.Days,
		EVBps: f.EVBps, EVCI: intervalOf(f.EVCI), Degraded: f.Degraded}, ForwardEvaluated
}

// rfc3339 returns s as UTC RFC 3339, or "" when s is not a time: the response
// never carries stored text that is not what the application wrote.
func rfc3339(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
