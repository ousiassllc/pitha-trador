package trialflow_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/shadow"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
	"github.com/ousiassllc/pitha-trador/internal/service/rag"
)

// reservationOf is what the trial reserves for one call of the test state: the
// request's estimated input tokens at the worst-case price.
func reservationOf(t *testing.T, arm jevtrialsettings.Arm) float64 {
	t.Helper()
	n, err := trialwire.RequestBytes(arm.Model, nil, jev.TraderRequest{QuestionVersion: arm.PromptVersion, State: state(time.Time{}), RAGContext: rag.Context{}})
	if err != nil {
		t.Fatalf("RequestBytes: %v", err)
	}
	return float64(shadow.EstimateInputTokens(n)) * jevtrialsettings.WorstCaseUSDPerMtok / 1e6
}

// Reservations are given back once a row is stored: afterwards the stored cost
// alone decides, so a cap with room is not blocked by calls that are over.
func TestReservationsAreReleasedAfterTheRowIsStored(t *testing.T) {
	e := newEnv(t, metalabelsettings.ModePaper, nil)
	arm := defaultArm("a1")
	costly := okResp()
	costly.ReportedCostUSD = new(0.0015)
	e.addArm("a1", answer(costly, 3000))
	e.enable(arm)
	// Room for 4 reservations (about 0.0016 each) in total. Each stored row counts
	// 0.0015: after 3 rows a fourth call still fits (0.0045 + a reservation), after 4
	// rows nothing does. A leaked reservation would stop the calls earlier.
	e.set(jevtrialsettings.KeyDailyCostCapUSD, strconv.FormatFloat(4*reservationOf(t, arm), 'g', -1, 64))
	for range 6 {
		e.evaluate()
	}
	got := e.arms["a1"].calls()
	if got < 3 || got > 4 {
		t.Fatalf("calls = %d, want 3..4 (the stored cost plus a reservation stops the arm, a leaked reservation would stop it earlier)", got)
	}
	if n := e.count(`SELECT COUNT(*) FROM jev_trial_decisions`); n != got {
		t.Errorf("rows = %d, want %d: every call that was made is stored", n, got)
	}
	// A leaked reservation would block a new day's first call as well.
	e.advance(24 * time.Hour)
	before := e.arms["a1"].calls()
	e.evaluate()
	if e.arms["a1"].calls() != before+1 {
		t.Error("the next day's call was blocked: a reservation leaked")
	}
}
