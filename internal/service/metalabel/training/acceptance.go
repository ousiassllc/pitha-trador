package training

import (
	"math"
	"math/rand/v2"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/metalabel"
)

// Reasons recorded in metalabel_models.reject_reason for a model that learned
// but was not adopted (issue #780, FR-META-7).
const (
	RejectOOSSamples = "oos_samples_below_minimum"
	// RejectOOSDays: the OOS rows span too few trading days for the cluster
	// bootstrap to mean anything.
	RejectOOSDays = "oos_days_below_minimum"
	RejectTakes   = "oos_takes_below_minimum"
	// RejectEV: the lower confidence limit of the OOS EV is not above the bar.
	RejectEV = "oos_ev_ci_not_positive"
	// RejectAUC: the lower confidence limit of the OOS AUC is not above the bar.
	RejectAUC = "oos_auc_ci_not_above_minimum"
	// RejectNotBetter: the active model is not beaten on the same OOS rows
	// (significantly, while it still passes the bar itself).
	RejectNotBetter = "candidate_not_better_than_active"
	// RejectNotComparable: the active model could not be evaluated on the
	// candidate's OOS rows, so the current model is kept.
	RejectNotComparable = "active_model_not_comparable"
)

// The assumptions the default sample sizes are derived from (documented in
// FR-META-7; RequiredTakes is the formula).
const (
	// AssumedSigmaBps is the standard deviation of one taken signal's net
	// return: bounded by the triple-barrier TP/SL (30 bps each by default).
	AssumedSigmaBps = 30.0
	// AssumedEdgeBps is the mean net EV per take the model is assumed to have
	// when it is worth adopting (about a sixth of sigma).
	AssumedEdgeBps = 5.0
	// AssumedPower is the chance the bar detects that edge.
	AssumedPower = 0.80
	// AssumedDesignEffect inflates the sample for the correlation of same-day
	// and same-symbol decisions (1 + (m-1)*rho for m decisions per cluster).
	AssumedDesignEffect = 1.5
	// AssumedTakeRate is the share of OOS decisions the model takes.
	AssumedTakeRate = 0.5
	// DefaultMinOOSDays is the fewest trading days the bootstrap resamples.
	DefaultMinOOSDays = 5
)

// AcceptanceConfig is the OOS bar a freshly trained model must clear to be
// adopted. A model below it is still stored (status rejected) so its metrics
// stay on record, but LatestActiveModel never returns it, so the previous
// model (or, with none, a skip) stays in force.
//
// The bar is judged on confidence intervals, not point estimates: the lower
// limit of the OOS EV and AUC (cluster bootstrap over same-day, same-symbol
// decisions) must clear it, on enough samples, takes and days.
type AcceptanceConfig struct {
	// MinOOSSamples is the fewest scored OOS rows an evaluation is trusted on.
	MinOOSSamples int
	// MinTakes is the fewest OOS signals the model must have taken.
	MinTakes int
	// MinOOSDays is the fewest distinct JST trading days of OOS rows.
	MinOOSDays int
	// MinAUCLower is the value the lower confidence limit of the OOS AUC must
	// exceed (0.5 is no better than chance); MinEVLowerBps the same for the EV.
	MinAUCLower   float64
	MinEVLowerBps float64
	// Alpha is the one-sided tail of the intervals and Resamples the bootstrap size.
	Alpha     float64
	Resamples int
}

// RequiredTakes is the number of independent taken signals needed for a
// one-sided test at level alpha to detect a mean net return of edgeBps with the
// given power when one signal's standard deviation is sigmaBps, inflated by the
// design effect of the clustering: ceil(((z(1-alpha)+z(power))*sigma/edge)^2 * deff).
func RequiredTakes(sigmaBps, edgeBps, alpha, power, designEffect float64) int {
	n := math.Pow((zScore(1-alpha)+zScore(power))*sigmaBps/edgeBps, 2) * designEffect
	return int(math.Ceil(n))
}

// zScore is the standard normal quantile of p.
func zScore(p float64) float64 { return math.Sqrt2 * math.Erfinv(2*p-1) }

// DefaultAcceptanceConfig returns the derived bar (docs FR-META-7): 542 takes
// (RequiredTakes for sigma 30 bps, edge 5 bps, alpha 1%, power 80%, design
// effect 1.5) out of at least 1084 OOS samples (a 50% take rate) on at least 5
// trading days, with the lower limits of the 98% cluster-bootstrap intervals of
// the EV above 0 and of the AUC above 0.5.
func DefaultAcceptanceConfig() AcceptanceConfig {
	takes := RequiredTakes(AssumedSigmaBps, AssumedEdgeBps, DefaultAlpha, AssumedPower, AssumedDesignEffect)
	return AcceptanceConfig{
		MinOOSSamples: int(math.Ceil(float64(takes) / AssumedTakeRate)),
		MinTakes:      takes,
		MinOOSDays:    DefaultMinOOSDays,
		MinAUCLower:   0.5,
		MinEVLowerBps: 0,
		Alpha:         DefaultAlpha,
		Resamples:     DefaultResamples,
	}
}

// criteria is the record stored with a judged model.
func (c AcceptanceConfig) criteria(seed uint64) repo.Criteria {
	return repo.Criteria{
		MinOOSSamples: c.MinOOSSamples, MinTakes: c.MinTakes, MinOOSDays: c.MinOOSDays,
		MinEVLowerBps: c.MinEVLowerBps, MinAUCLower: c.MinAUCLower,
		Alpha: c.Alpha, Resamples: c.Resamples, Seed: seed,
	}
}

// Intervals are the cluster-bootstrap confidence intervals of an evaluation.
type Intervals struct {
	// Days is the number of JST trading days the OOS rows span.
	Days int
	// EV is the interval of the mean net return per taken signal and AUC that
	// of the OOS AUC; nil when undefined (too few days, takes or one class).
	EV, AUC *repo.Interval
}

// newRNG is a seeded, reproducible generator: the bootstrap needs repeatability,
// not unpredictability.
func newRNG(seed, stream uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, stream)) //nolint:gosec // G404: a reproducible resampling stream, not a secret
}

// Intervals computes the intervals of e under c with a reproducible seed.
func (e Evaluation) Intervals(c AcceptanceConfig, seed uint64) Intervals {
	cl := newClusters(e.scored)
	return Intervals{
		Days: cl.days(),
		EV:   e.evInterval(cl, c, seed),
		AUC:  aucInterval(cl, e.scored, c.Resamples, c.Alpha, newRNG(seed, 2)),
	}
}

// evInterval is the interval of the mean net return per taken signal.
func (e Evaluation) evInterval(cl clusters, c AcceptanceConfig, seed uint64) *repo.Interval {
	sum, n := make([]float64, cl.n), make([]float64, cl.n)
	for i, r := range e.scored {
		if r.take {
			sum[cl.of[i]] += r.ret
			n[cl.of[i]]++
		}
	}
	return ratioInterval(cl, sum, n, c.Resamples, c.Alpha, newRNG(seed, 1))
}

// Judge returns repo.ModelActive with an empty reason when e clears the bar,
// otherwise repo.ModelRejected with why. It checks the cheap sample counts
// first, then the lower confidence limits of the EV and the AUC.
func (c AcceptanceConfig) Judge(e Evaluation, iv Intervals) (status, reason string) {
	switch {
	case e.Samples < c.MinOOSSamples:
		return repo.ModelRejected, RejectOOSSamples
	case iv.Days < c.MinOOSDays:
		return repo.ModelRejected, RejectOOSDays
	case e.Takes < c.MinTakes:
		return repo.ModelRejected, RejectTakes
	case iv.EV == nil || !(iv.EV.Low > c.MinEVLowerBps):
		return repo.ModelRejected, RejectEV
	case iv.AUC == nil || !(iv.AUC.Low > c.MinAUCLower):
		return repo.ModelRejected, RejectAUC
	}
	return repo.ModelActive, ""
}
