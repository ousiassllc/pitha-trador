// Package calibrationgate is the Policy Engine's "does the confidence bucket hold
// enough labeled samples" check (FR-POLICY-3 "キャリブレーション対象外") over the
// live Trader prompt version's samples (issue #881). It lives beside
// internal/service/policy to keep that directory within the repository's size
// limit, and has no dependency on it.
//
// The samples counted are those of the live version's cohort
// (internal/service/jev cohort.go: trader-v1..v3 are one). While they are too
// few, Paper alone may decide on the total over every version; Live, and any
// mode that is not Paper, never does - its decision does not pass. Live's
// conditions are not loosened.
package calibrationgate

import (
	"context"
	"fmt"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// Source answers whether a confidence bucket is populated enough
// (internal/service/calibration.Service satisfies it).
type Source interface {
	// BucketHasSamples reports whether the confidence bucket containing
	// confidence holds at least min labeled samples of the live Trader prompt
	// version's cohort (false when it is in no bucket). It must be cheap and
	// independent of the labeling history's size: it runs on every directional
	// Jev trader job.
	BucketHasSamples(ctx context.Context, confidence float64, min int) (bool, error)
}

// Verdict is the result of Gate.Check.
type Verdict struct {
	// OK: the bucket holds enough samples to trust the confidence.
	OK bool
	// Detail says the live version's own samples were too few, naming it. It is
	// empty when they were enough. With OK it means Paper's fallback judged the
	// bucket on the all-versions total (UsedAllVersions).
	Detail string
}

// UsedAllVersions reports that OK holds only because Paper's all-versions
// total reached the minimum while the live version's own samples did not.
func (v Verdict) UsedAllVersions() bool { return v.OK && v.Detail != "" }

// Gate checks a bucket against Source for Mode.
type Gate struct {
	Source Source
	// AllVersions counts the samples of every Trader version (the same service
	// over a repository that ignores the cohort scope); nil means no fallback.
	AllVersions Source
	// Mode is the operating mode; only metalabelsettings.ModePaper may fall back
	// to the AllVersions total.
	Mode metalabelsettings.Mode
	// Live returns the live version, named in Verdict.Detail
	// (jev.ResolveLiveVersion bound to the settings store); nil leaves it unnamed.
	Live func(ctx context.Context) (string, error)
}

// Check reports whether the bucket containing confidence holds at least min
// labeled samples of the live version's cohort, or, for Paper alone, of every
// version when the live version's own are too few. A failed count, or a failed
// read of the live version for the reason, is an error, never a pass.
func (g Gate) Check(ctx context.Context, confidence float64, min int) (Verdict, error) {
	enough, err := g.Source.BucketHasSamples(ctx, confidence, min)
	if err != nil {
		return Verdict{}, fmt.Errorf("count calibration samples: %w", err)
	}
	if enough {
		return Verdict{OK: true}, nil
	}
	detail, err := g.detail(ctx, min)
	if err != nil {
		return Verdict{}, err
	}
	if g.AllVersions != nil && g.Mode == metalabelsettings.ModePaper {
		enoughAll, err := g.AllVersions.BucketHasSamples(ctx, confidence, min)
		if err != nil {
			return Verdict{}, fmt.Errorf("count calibration samples of every version: %w", err)
		}
		return Verdict{OK: enoughAll, Detail: detail}, nil
	}
	return Verdict{Detail: detail}, nil
}

// detail is the text saying the live version's own samples are too few.
func (g Gate) detail(ctx context.Context, min int) (string, error) {
	version := "live version"
	if g.Live != nil {
		v, err := g.Live(ctx)
		if err != nil {
			return "", fmt.Errorf("resolve the live version for the calibration reason: %w", err)
		}
		version = "live version " + v
	}
	return fmt.Sprintf("%s has fewer than %d labeled samples in the confidence bucket", version, min), nil
}
