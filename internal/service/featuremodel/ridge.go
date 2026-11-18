package featuremodel

import (
	"errors"
	"fmt"
	"math"
)

// normalEq accumulates the normal equations of one regression over rows streamed
// one at a time (no row is kept): A = X'X and b = X'y with X = [1, z...] (the
// intercept column first). Rows are added in the caller's fixed order, so the
// result is reproducible bit for bit. float64(...) around every product stops the
// compiler from fusing it into the following addition (FMA on some architectures),
// which would change the rounding between platforms.
type normalEq struct {
	dim int
	a   []float64 // dim*dim, row-major; only j >= i is filled
	b   []float64
	n   int
}

func newNormalEq(features int) *normalEq {
	dim := features + 1
	return &normalEq{dim: dim, a: make([]float64, dim*dim), b: make([]float64, dim)}
}

// add accumulates one row: z are the standardized features, y the target.
func (e *normalEq) add(z []float64, y float64) {
	e.n++
	// v[0] = 1 (the intercept), v[i] = z[i-1]
	for i := 0; i < e.dim; i++ {
		vi := 1.0
		if i > 0 {
			vi = z[i-1]
		}
		e.b[i] += float64(vi * y)
		for j := i; j < e.dim; j++ {
			vj := 1.0
			if j > 0 {
				vj = z[j-1]
			}
			e.a[i*e.dim+j] += float64(vi * vj)
		}
	}
}

// solve returns the ridge solution (intercept first): (X'X + lambda*D) w = X'y with D
// the identity except 0 for the intercept, by Cholesky decomposition.
func (e *normalEq) solve(lambda float64) ([]float64, error) {
	d := e.dim
	l := make([]float64, d*d) // lower triangle of the Cholesky factor
	for i := 0; i < d; i++ {
		for j := 0; j <= i; j++ {
			sum := e.a[j*d+i] // symmetric: the filled half is j <= i read as (j, i)
			if i == j && i > 0 {
				sum += lambda
			}
			for k := 0; k < j; k++ {
				sum -= float64(l[i*d+k] * l[j*d+k])
			}
			if i == j {
				if sum <= 0 || math.IsNaN(sum) {
					return nil, errors.New("featuremodel: normal equations are not positive definite")
				}
				l[i*d+i] = math.Sqrt(sum)
			} else {
				l[i*d+j] = sum / l[j*d+j]
			}
		}
	}
	// L u = b, then L' w = u.
	u := make([]float64, d)
	for i := 0; i < d; i++ {
		sum := e.b[i]
		for k := 0; k < i; k++ {
			sum -= float64(l[i*d+k] * u[k])
		}
		u[i] = sum / l[i*d+i]
	}
	w := make([]float64, d)
	for i := d - 1; i >= 0; i-- {
		sum := u[i]
		for k := i + 1; k < d; k++ {
			sum -= float64(l[k*d+i] * w[k])
		}
		w[i] = sum / l[i*d+i]
	}
	for _, v := range w {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("featuremodel: ridge solution is not finite")
		}
	}
	return w, nil
}

// welford is the streaming mean and population variance of the observed values of
// one feature.
type welford struct {
	n        int
	mean, m2 float64
}

func (w *welford) add(x float64) {
	w.n++
	d := x - w.mean
	w.mean += d / float64(w.n)
	w.m2 += float64(d * (x - w.mean))
}

// meanStd returns the mean and standard deviation to standardize with: a feature
// with no observed value or no variance gets mean (0 if none) and std 1.
func (w *welford) meanStd() (mean, std float64) {
	if w.n == 0 {
		return 0, 1
	}
	std = math.Sqrt(w.m2 / float64(w.n))
	if std < minStd || math.IsNaN(std) {
		std = 1
	}
	return w.mean, std
}
