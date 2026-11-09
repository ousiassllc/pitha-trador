package adoptionview

// checks evaluates the five numeric criteria of training.AcceptanceConfig.Judge
// with its inequalities: the sample, day and take counts must reach the
// minimum (>=); the lower limit of the EV and AUC intervals must exceed it (>).
// criteria is nil unless recorded; the take and day results are only known
// then (v.OOSTakes / v.OOSDays are nil otherwise).
func checks(s Stored, c *Criteria, v View) Checks {
	var req struct{ samples, days, takes, ev, auc *float64 }
	if c != nil {
		req.samples, req.days, req.takes = ptr(float64(c.MinOOSSamples)), ptr(float64(c.MinOOSDays)), ptr(float64(c.MinTakes))
		req.ev, req.auc = ptr(c.MinEVLowerBps), ptr(c.MinAUCLower)
	}
	var takes, days, evLow, aucLow *float64
	if v.OOSTakes != nil {
		takes, days = ptr(float64(*v.OOSTakes)), ptr(float64(*v.OOSDays))
	}
	if v.OOSEVCI != nil {
		evLow = ptr(v.OOSEVCI.Low)
	}
	if v.OOSAUCCI != nil {
		aucLow = ptr(v.OOSAUCCI.Low)
	}
	return Checks{
		OOSSamples:  check(ptr(float64(s.OOSSamples)), req.samples, false),
		OOSDays:     check(days, req.days, false),
		OOSTakes:    check(takes, req.takes, false),
		OOSEVCILow:  check(evLow, req.ev, true),
		OOSAUCCILow: check(aucLow, req.auc, true),
	}
}

// check compares actual with required: actual > required when strict, else
// actual >= required. Met is nil when either is unknown.
func check(actual, required *float64, strict bool) Check {
	c := Check{Actual: actual, Required: required}
	if actual == nil || required == nil {
		return c
	}
	met := *actual >= *required
	if strict {
		met = *actual > *required
	}
	c.Met = &met
	return c
}

// firstUnmet is the first check, in Judge's order, with met = false, and
// whether every check is known (met is true or false).
func (c Checks) firstUnmet() (first *string, complete bool) {
	complete = true
	for _, n := range []struct {
		name  string
		check Check
	}{{CheckOOSSamples, c.OOSSamples}, {CheckOOSDays, c.OOSDays}, {CheckOOSTakes, c.OOSTakes},
		{CheckOOSEVCILow, c.OOSEVCILow}, {CheckOOSAUCCILow, c.OOSAUCCILow}} {
		switch {
		case n.check.Met == nil:
			complete = false
		case !*n.check.Met && first == nil:
			name := n.name
			first = &name
		}
	}
	return first, complete
}

func ptr[T any](v T) *T { return &v }
