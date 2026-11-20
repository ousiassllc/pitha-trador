package slicediag

import (
	"slices"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/evaldiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
)

// Bucket names that more than one dimension uses.
const (
	bucketUnknown = "unknown"
	bucketOther   = "other"
)

// Dimension names.
const (
	DimRegime         = "regime"
	DimTimeOfDay      = "time_of_day"
	DimDirection      = "direction"
	DimConfidenceBand = "confidence_band"
	DimWeekday        = "weekday"
	DimVolatilityBand = "volatility_band"
	DimTurnoverBand   = "turnover_band"
	DimSpreadBand     = "spread_band"
	DimSector         = "sector"
)

// assignment is how one dimension cuts a cohort's rows: labels[i] is the bucket of
// row i, order lists the buckets in output order (fixed, never by a result), and
// edges are the tercile boundaries of a tercile dimension.
type assignment struct {
	labels []string
	order  []string
	edges  []float64
}

// dimension is one cut. The slice below is the fixed registration: its order is
// the output order, and a dimension's family is part of the registration.
type dimension struct {
	name, family string
	assign       func(rows []evaldiag.Row) assignment
}

// dimensions: the preregistered family was fixed on PreregisteredOn for
// PreregisteredIssue, before any result was looked at; the exploratory one is what
// was added to look at after the fact.
var dimensions = []dimension{
	{DimRegime, FamilyPreregistered, fixed([]string{"trend", "mean_revert", bucketUnknown}, regimeOf)},
	{DimTimeOfDay, FamilyPreregistered, fixed([]string{"open", "morning", "afternoon", "pre_close", bucketOther}, timeOfDayOf)},
	{DimDirection, FamilyPreregistered, fixed([]string{domain.JevDirectionLong, domain.JevDirectionShort}, func(r evaldiag.Row) string { return r.Direction })},
	{DimConfidenceBand, FamilyPreregistered, fixed(append(slices.Clone(confidenceBands), bucketUnknown), confidenceBandOf)},
	{DimWeekday, FamilyExploratory, fixed([]string{"mon", "tue", "wed", "thu", "fri", bucketOther}, weekdayOf)},
	{DimVolatilityBand, FamilyExploratory, terciles(func(a evaldiag.Attrs) *float64 { return a.VolatilityBps })},
	{DimTurnoverBand, FamilyExploratory, terciles(func(a evaldiag.Attrs) *float64 { return a.Turnover5m })},
	{DimSpreadBand, FamilyExploratory, terciles(func(a evaldiag.Attrs) *float64 { return a.SpreadBps })},
	{DimSector, FamilyExploratory, sectors},
}

// Names are the dimension names in output order.
func Names() []string {
	out := make([]string, len(dimensions))
	for i, d := range dimensions {
		out[i] = d.name
	}
	return out
}

// namesOf are the names of the family's dimensions in output order.
func namesOf(family string) []string {
	var out []string
	for _, d := range dimensions {
		if d.family == family {
			out = append(out, d.name)
		}
	}
	return out
}

// fixed is a dimension whose buckets are known in advance: label gives each row's
// bucket, which must be one of order.
func fixed(order []string, label func(evaldiag.Row) string) func([]evaldiag.Row) assignment {
	return func(rows []evaldiag.Row) assignment {
		a := assignment{labels: make([]string, len(rows)), order: order}
		for i, r := range rows {
			a.labels[i] = label(r)
		}
		return a
	}
}

// regimeOf is the decision's recorded market regime; anything not recorded (or not
// one of the two the column holds) is unknown.
func regimeOf(r evaldiag.Row) string {
	switch r.Attrs.Regime {
	case "trend", "mean_revert":
		return r.Attrs.Regime
	}
	return bucketUnknown
}

// Time-of-day sessions of the JST decision time: half-open [from, to) in seconds of the day.
var sessions = []struct {
	name     string
	from, to int
}{
	{"open", 9 * 3600, 9*3600 + 30*60},
	{"morning", 9*3600 + 30*60, 11*3600 + 30*60},
	{"afternoon", 12*3600 + 30*60, 14*3600 + 30*60},
	{"pre_close", 14*3600 + 30*60, 15*3600 + 30*60},
}

func timeOfDayOf(r evaldiag.Row) string {
	t := r.At.In(query.JST)
	s := t.Hour()*3600 + t.Minute()*60 + t.Second()
	for _, w := range sessions {
		if s >= w.from && s < w.to {
			return w.name
		}
	}
	return bucketOther
}

// confidenceBands are the fixed bands [0,0.6) [0.6,0.7) [0.7,0.8) [0.8,0.9) [0.9,1].
var confidenceBands = []string{"[0.0,0.6)", "[0.6,0.7)", "[0.7,0.8)", "[0.8,0.9)", "[0.9,1.0]"}

// confidenceEdges are the lower edges of confidenceBands.
var confidenceEdges = []float64{0, 0.6, 0.7, 0.8, 0.9}

func confidenceBandOf(r evaldiag.Row) string {
	c := r.Attrs.Confidence
	if c == nil || !(*c >= 0 && *c <= 1) { // also NaN
		return bucketUnknown
	}
	for i := len(confidenceEdges) - 1; i >= 0; i-- {
		if *c >= confidenceEdges[i] {
			return confidenceBands[i]
		}
	}
	return bucketUnknown
}

func weekdayOf(r evaldiag.Row) string {
	switch r.At.In(query.JST).Weekday() {
	case time.Monday:
		return "mon"
	case time.Tuesday:
		return "tue"
	case time.Wednesday:
		return "wed"
	case time.Thursday:
		return "thu"
	case time.Friday:
		return "fri"
	}
	return bucketOther
}
