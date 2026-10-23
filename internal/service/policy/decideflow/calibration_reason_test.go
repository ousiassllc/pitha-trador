package decideflow_test

import (
	"context"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

// Issue #881: the legacy rule's not_calibrated names the live version whose own
// samples were too few; without a detail the reason is the bare code, as before.
func TestEngine_Decide_NotCalibratedNamesTheLiveVersion(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)
	for name, tc := range map[string]struct{ detail, want string }{
		"no detail":         {"", policy.ReasonNotCalibrated},
		"names the version": {"live version trader-v4 has fewer than 20 labeled samples in the confidence bucket", policy.ReasonNotCalibrated + ": live version trader-v4 has fewer than 20 labeled samples in the confidence bucket"},
	} {
		t.Run(name, func(t *testing.T) {
			in := passingInput(domain.JevDirectionLong)
			in.Calibrated, in.CalibrationDetail = false, tc.detail
			sig := e.Decide(context.Background(), in)
			if sig.Direction != domain.JevDirectionNone || sig.RiskPassed || sig.RejectReason == nil || *sig.RejectReason != tc.want {
				t.Fatalf("signal = %+v (%v), want NONE %q", sig, sig.RejectReason, tc.want)
			}
		})
	}
}
