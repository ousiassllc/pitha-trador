package gbt

import (
	"fmt"
	"math"
	"slices"
	"sort"
)

// minGain is the least variance reduction a split must bring.
const minGain = 1e-9

// fitter holds the binned training rows and the running state of the boosting.
type fitter struct {
	p    Params
	d    int         // features
	n    int         // rows
	y    []float64   // targets
	bin  []uint8     // n*d, row-major: the bin of each value
	cuts [][]float64 // per feature, ascending: bin b holds v <= cuts[b] (and > cuts[b-1])
	pred []float64   // the current prediction of each row
	res  []float64   // y - pred at the start of the current tree
	sum  []float64   // histogram scratch: d*(Bins) sums of res
	cnt  []int       // histogram scratch: d*(Bins) counts
}

func newFitter(x [][]float64, y []float64, p Params) (*fitter, error) {
	f := &fitter{p: p, n: len(x), d: len(x[0]), y: y}
	f.cuts = make([][]float64, f.d)
	f.bin = make([]uint8, f.n*f.d)
	col := make([]float64, f.n)
	for j := range f.d {
		for i, row := range x {
			if len(row) != f.d {
				return nil, fmt.Errorf("gbt: row %d has %d values, want %d", i, len(row), f.d)
			}
			if math.IsNaN(row[j]) || math.IsInf(row[j], 0) {
				return nil, fmt.Errorf("gbt: row %d feature %d is not finite", i, j)
			}
			col[i] = row[j]
		}
		f.cuts[j] = quantileCuts(col, p.Bins)
		for i, row := range x {
			f.bin[i*f.d+j] = uint8(sort.SearchFloat64s(f.cuts[j], row[j])) //nolint:gosec // G115: at most Bins-1 cuts and Bins <= MaxBins (255); first cut >= value
		}
	}
	f.pred, f.res = make([]float64, f.n), make([]float64, f.n)
	f.sum, f.cnt = make([]float64, f.d*p.Bins), make([]int, f.d*p.Bins)
	return f, nil
}

// quantileCuts returns up to bins-1 ascending, distinct cut values at the quantiles
// of col, which it sorts in place (the caller passes scratch). No cut equals the
// maximum, so every cut has values on its right.
func quantileCuts(col []float64, bins int) []float64 {
	slices.Sort(col)
	n := len(col)
	var cuts []float64
	for k := 1; k < bins; k++ {
		v := col[max(k*n/bins-1, 0)]
		if v >= col[n-1] {
			break
		}
		if len(cuts) == 0 || v > cuts[len(cuts)-1] {
			cuts = append(cuts, v)
		}
	}
	return cuts
}

// base is the mean target, summed in row order.
func (f *fitter) base() float64 {
	s := 0.0
	for _, v := range f.y {
		s += v
	}
	return s / float64(f.n)
}

// growTree builds one tree on the current residuals and adds its (shrunk) leaf values
// to every row's prediction.
func (f *fitter) growTree() Tree {
	idx := make([]int32, f.n)
	for i := range idx {
		idx[i] = int32(i)
	}
	var t Tree
	f.grow(&t, idx, 0)
	return t
}

// grow appends the node for the rows idx and its subtree to t and returns its index.
func (f *fitter) grow(t *Tree, idx []int32, depth int) int {
	at := len(*t)
	*t = append(*t, Node{Feature: -1})
	total, feature, b, ok := 0.0, 0, 0, false
	if depth < f.p.Depth && len(idx) >= 2*f.p.MinLeaf {
		total, feature, b, ok = f.bestSplit(idx)
	} else {
		total = f.rowsSum(idx)
	}
	if !ok {
		v := total / float64(len(idx))
		(*t)[at].Value = v
		for _, i := range idx {
			f.pred[i] = float64(f.p.Rate*v) + f.pred[i]
		}
		return at
	}
	var left, right []int32
	for _, i := range idx {
		if int(f.bin[int(i)*f.d+feature]) <= b {
			left = append(left, i)
		} else {
			right = append(right, i)
		}
	}
	(*t)[at].Feature, (*t)[at].Threshold = feature, f.cuts[feature][b]
	l := f.grow(t, left, depth+1)
	r := f.grow(t, right, depth+1)
	(*t)[at].Left, (*t)[at].Right = l, r
	return at
}

func (f *fitter) rowsSum(idx []int32) float64 {
	s := 0.0
	for _, i := range idx {
		s += f.res[i]
	}
	return s
}

// bestSplit returns the node's residual sum and the split (feature, bin: left holds
// bins <= b) with the largest variance reduction among those leaving MinLeaf rows on
// each side; ok is false when there is none. Ties keep the lowest feature, then the
// lowest bin.
func (f *fitter) bestSplit(idx []int32) (total float64, feature, b int, ok bool) {
	nb := f.p.Bins
	clear(f.sum)
	clear(f.cnt)
	for _, i := range idx {
		r := f.res[i]
		total += r
		row := f.bin[int(i)*f.d : int(i)*f.d+f.d]
		for j, v := range row {
			f.sum[j*nb+int(v)] += r
			f.cnt[j*nb+int(v)]++
		}
	}
	n := float64(len(idx))
	parent := total * total / n
	best := minGain
	for j := range f.d {
		cl, sl := 0, 0.0
		for c := range f.cuts[j] {
			cl += f.cnt[j*nb+c]
			sl += f.sum[j*nb+c]
			cr := len(idx) - cl
			if cl < f.p.MinLeaf {
				continue
			}
			if cr < f.p.MinLeaf {
				break
			}
			sr := total - sl
			gain := sl*sl/float64(cl) + sr*sr/float64(cr) - parent
			if gain > best {
				best, feature, b, ok = gain, j, c, true
			}
		}
	}
	return total, feature, b, ok
}
