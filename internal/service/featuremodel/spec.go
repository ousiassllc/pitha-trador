package featuremodel

import (
	"fmt"
	"slices"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/featuremodel"
)

// AlgorithmTree is research_model_runs.algorithm of the gradient-boosted tree model
// (internal/service/featuremodel/gbt); the ridge model's is Algorithm.
const AlgorithmTree = "gbt"

// Spec is one trained model: an algorithm learning the labels of one horizon. Its
// run version is rm-<algorithm>1-<horizon>-<predicted day> (rm-ridge1-30m-...,
// rm-gbt1-60m-...); a change of method is a new number in it. The candidate ledger
// names the model by algorithm and horizon (research_model:<algorithm>:<horizon>).
type Spec struct {
	Algorithm, Horizon string
}

// Baseline is the ridge model of the 30-minute label that was the first and, until
// the tree model and the other horizons were added, only model. A Runner trains just
// it unless WithSpecs says otherwise.
var Baseline = Spec{Algorithm: Algorithm, Horizon: Horizon}

// Specs returns every model the nightly task trains, in the fixed order they are
// tried: for each horizon (30m, 60m, close) the ridge model, then the tree model.
func Specs() []Spec {
	var out []Spec
	for _, h := range repo.Horizons {
		out = append(out, Spec{Algorithm, h}, Spec{AlgorithmTree, h})
	}
	return out
}

// Version is research_model_runs.version of the run that predicts the JST day.
func (s Spec) Version(predictDay string) string { return s.prefix() + predictDay }

func (s Spec) prefix() string { return "rm-" + s.Algorithm + "1-" + s.Horizon + "-" }

func (s Spec) validate() error {
	if (s.Algorithm != Algorithm && s.Algorithm != AlgorithmTree) || !slices.Contains(repo.Horizons, s.Horizon) {
		return fmt.Errorf("featuremodel: unknown model %s/%s", s.Algorithm, s.Horizon)
	}
	return nil
}
