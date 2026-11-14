package trial

import "github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"

// reservation is the worst-case cost of one call that is about to be made or is
// in flight. The daily caps are judged on the stored rows' counted cost, which
// does not include a call that has not finished; without a reservation up to
// maxInFlight calls could all pass the same check and then all be stored on top
// of a cap that was already nearly reached. A reservation makes a call count
// against the caps from the moment it is allowed until its row is stored (or the
// call is abandoned), so the stored cost plus the reservations stay within the
// cap. The amount is reserved in memory only: after a restart no call is in
// flight.
type reservation struct {
	t   *Trial
	arm string
	usd float64
}

// reservedUSD is what a call of requestBytes is reserved at: the estimated
// input tokens of the request (shadow.EstimateInputTokens) at the worst-case
// price jevtrialsettings.WorstCaseUSDPerMtok, the same price a failed call that
// may have been billed is counted at (FailedCost). It is independent of the arm's
// configured list price, so a low list price cannot shrink it. Output tokens are
// not priced (see "Overshoot" in the spec).
func reservedUSD(requestBytes int) float64 {
	return worstCaseUSD(shadow.EstimateInputTokens(requestBytes))
}

// reserve adds usd for arm to the in-flight reservations and returns it.
func (t *Trial) reserve(arm string, usd float64) *reservation {
	t.resMu.Lock()
	defer t.resMu.Unlock()
	t.reservedTotal += usd
	t.reservedArm[arm] += usd
	return &reservation{t: t, arm: arm, usd: usd}
}

// reserved returns the amount reserved over all arms and for arm (this call's
// own reservation included). mayCall reads it BEFORE it reads the stored cost:
// a call that stores its row and releases in between is then counted twice (the
// safe side), never zero times.
func (t *Trial) reserved(arm string) (total, byArm float64) {
	t.resMu.Lock()
	defer t.resMu.Unlock()
	return t.reservedTotal, t.reservedArm[arm]
}

// release gives the reservation back; call it after the call's row is stored (or
// the failure counted) so that the cost never goes uncounted in between. A nil
// reservation and a second call are no-ops.
func (r *reservation) release() {
	if r == nil || r.t == nil {
		return
	}
	t := r.t
	r.t = nil
	t.resMu.Lock()
	defer t.resMu.Unlock()
	t.reservedTotal -= r.usd
	t.reservedArm[r.arm] -= r.usd
	if t.reservedArm[r.arm] <= 0 {
		delete(t.reservedArm, r.arm)
	}
	if len(t.reservedArm) == 0 {
		t.reservedTotal = 0 // no float drift once nothing is in flight
	}
}
