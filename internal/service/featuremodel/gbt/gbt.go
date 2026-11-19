// Package gbt is a small, deterministic gradient-boosted regression tree model in
// plain Go (squared-error loss, histogram splits) for the research feature model
// (internal/service/featuremodel). It has no dependency and no randomness: there is
// no row or feature subsampling, rows are visited in the caller's order, and every
// tie is broken towards the lowest feature and then the lowest bin, so the same rows
// and Params always give the same trees, bit for bit (floating-point products are
// wrapped in float64(...) so no platform fuses them into the following addition).
// The model reads only the numbers it is given.
package gbt

import (
	"errors"
	"fmt"
	"math"
)

// MaxBins is the most bins one feature can have: a bin index is a byte.
const MaxBins = 255

// Params are the hyperparameters of Fit.
type Params struct {
	// Trees is the number of boosting rounds and Depth the most splits from a root to a leaf.
	Trees, Depth int
	// Rate is the shrinkage applied to every tree (0 < Rate <= 1).
	Rate float64
	// MinLeaf is the fewest rows a leaf holds.
	MinLeaf int
	// Bins is the most quantile bins per feature (2..MaxBins).
	Bins int
}

func (p Params) validate() error {
	switch {
	case p.Trees < 1 || p.Depth < 1 || p.MinLeaf < 1:
		return fmt.Errorf("gbt: trees, depth and min leaf must be positive, got %d, %d, %d", p.Trees, p.Depth, p.MinLeaf)
	case p.Rate <= 0 || p.Rate > 1 || math.IsNaN(p.Rate):
		return fmt.Errorf("gbt: rate %v is not in (0, 1]", p.Rate)
	case p.Bins < 2 || p.Bins > MaxBins:
		return fmt.Errorf("gbt: bins %d is not in 2..%d", p.Bins, MaxBins)
	}
	return nil
}

// Node is one node of a Tree. A leaf has Feature -1 and carries Value; a split sends
// a row to Left when x[Feature] <= Threshold and to Right otherwise (indices into the
// Tree).
type Node struct {
	Feature   int     `json:"f"`
	Threshold float64 `json:"t,omitempty"`
	Left      int     `json:"l,omitempty"`
	Right     int     `json:"r,omitempty"`
	Value     float64 `json:"v,omitempty"`
}

// Tree is a regression tree; Tree[0] is the root.
type Tree []Node

// Predict returns the value of the leaf x falls in.
func (t Tree) Predict(x []float64) float64 {
	i := 0
	for t[i].Feature >= 0 {
		if x[t[i].Feature] <= t[i].Threshold {
			i = t[i].Left
		} else {
			i = t[i].Right
		}
	}
	return t[i].Value
}

// Model is the base value plus Rate times the sum of the trees.
type Model struct {
	Base  float64 `json:"base"`
	Rate  float64 `json:"rate"`
	Trees []Tree  `json:"trees"`
}

// Predict returns the model's value for a feature vector with no missing (NaN) entry;
// the caller imputes first.
func (m Model) Predict(x []float64) float64 {
	v := m.Base
	for _, t := range m.Trees {
		v = float64(m.Rate*t.Predict(x)) + v
	}
	return v
}

// Fit boosts Params.Trees trees on the rows x (all of one width, no NaN) and targets
// y. Rows are used in the given order.
func Fit(x [][]float64, y []float64, p Params) (Model, error) {
	if err := p.validate(); err != nil {
		return Model{}, err
	}
	if len(x) != len(y) {
		return Model{}, fmt.Errorf("gbt: %d rows but %d targets", len(x), len(y))
	}
	if len(x) < 2*p.MinLeaf {
		return Model{}, fmt.Errorf("gbt: %d rows cannot make two leaves of %d rows", len(x), p.MinLeaf)
	}
	f, err := newFitter(x, y, p)
	if err != nil {
		return Model{}, err
	}
	m := Model{Base: f.base(), Rate: p.Rate, Trees: make([]Tree, 0, p.Trees)}
	for i := range f.pred {
		f.pred[i] = m.Base
	}
	for range p.Trees {
		for i := range f.res {
			f.res[i] = y[i] - f.pred[i]
		}
		m.Trees = append(m.Trees, f.growTree())
	}
	for _, t := range m.Trees {
		for _, n := range t {
			if math.IsNaN(n.Value) || math.IsInf(n.Value, 0) {
				return Model{}, errors.New("gbt: the fit is not finite")
			}
		}
	}
	return m, nil
}
