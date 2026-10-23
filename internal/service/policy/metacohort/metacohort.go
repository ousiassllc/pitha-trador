// Package metacohort compares the Trader prompt cohort a meta-labeling model was
// trained on with the live version's (issue #881). The model reads the
// entry_quality answer as a feature, so a model trained on another cohort's
// answers scores the live version's answers with the wrong meaning. It lives
// beside internal/service/policy to keep that directory within the repository's
// size limit, and has no dependency on it.
//
// On a mismatch Paper keeps using the model and the decision says so; any other
// mode does not use the model (the entry is not_calibrated). A model that
// recorded no cohort (one stored before the cohort was recorded) is a model whose
// training version is unknown, which counts as a mismatch in every mode: Live
// does not use it until a new training records its cohort and is adopted.
package metacohort

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
)

// Source returns the live Trader prompt version and its cohort (jev.LiveCohort
// bound to the settings store is one).
type Source func(ctx context.Context) (version, cohort string, err error)

// Checker compares a model's cohort with the live version's for Mode. The zero
// Checker has no mode (so it is not Paper) and no source: it never lets a model
// through. A nil Live is a live cohort that cannot be read.
type Checker struct {
	Mode metalabelsettings.Mode
	Live Source
}

// Verdict is the result of Check; at most one field is set.
type Verdict struct {
	// Mismatch: the model was trained on another cohort and the mode lets it keep
	// scoring (Paper); the decision records it.
	Mismatch string
	// Uncalibrated: the same condition in a mode that must not use such a model
	// (Live): the entry is not_calibrated.
	Uncalibrated string
	// Unavailable: the live cohort could not be read in a mode that must not
	// guess (Live): the entry is skipped.
	Unavailable string
}

// Check compares the cohort the model modelVersion was trained on (trained; ""
// for a model that recorded none) with the live version's. A live cohort that
// cannot be read is logged and, outside Paper, makes the entry unavailable.
func (c Checker) Check(ctx context.Context, modelVersion, trained string) Verdict {
	paper := c.Mode == metalabelsettings.ModePaper
	version, live, err := c.read(ctx)
	if err != nil {
		slog.Error("policy: read the live prompt cohort to check the meta-label model against", "error", err)
		if !paper {
			return Verdict{Unavailable: fmt.Sprintf("model cohort: %v", err)}
		}
		return Verdict{}
	}
	if trained != "" && trained == live { // a model with no recorded cohort never matches
		return Verdict{}
	}
	detail := mismatchDetail(modelVersion, trained, version, live)
	if paper {
		return Verdict{Mismatch: detail}
	}
	return Verdict{Uncalibrated: detail}
}

// mismatchDetail names the model and the live version; a model with no recorded
// training cohort says so instead of naming a cohort.
func mismatchDetail(modelVersion, trained, liveVersion, liveCohort string) string {
	if trained == "" {
		return fmt.Sprintf("meta model %s has no recorded training version (cohort), live version %s is cohort %s", modelVersion, liveVersion, liveCohort)
	}
	return fmt.Sprintf("meta model %s was trained on cohort %s, live version %s is cohort %s", modelVersion, trained, liveVersion, liveCohort)
}

func (c Checker) read(ctx context.Context) (version, cohort string, err error) {
	if c.Live == nil {
		return "", "", errNoSource
	}
	return c.Live(ctx)
}

var errNoSource = errors.New("no live cohort source configured")
