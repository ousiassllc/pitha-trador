package policy_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func TestEngine_Decide_HugeAPIErrorBodyKeepsRejectReasonWithinColumnWidth(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)
	in := passingInput(domain.JevDirectionLong)
	in.APIErr = &jev.APIError{StatusCode: http.StatusBadGateway, Body: strings.Repeat("<h1>Bad Gateway</h1>\n", 50_000)}

	sig := e.Decide(context.Background(), in)

	if sig.RejectReason == nil || !strings.HasPrefix(*sig.RejectReason, policy.ReasonAPIError) {
		t.Fatalf("RejectReason = %v, want prefix %q", sig.RejectReason, policy.ReasonAPIError)
	}
	if got := len(*sig.RejectReason); got > 255 {
		t.Fatalf("len(RejectReason) = %d, want <= 255 (trade_signals.reject_reason VARCHAR(255))", got)
	}
}

func TestEngine_Decide_LongRejectReasonIsTruncatedOnRuneBoundary(t *testing.T) {
	e := policy.NewEngine(testThresholds(), nil, nil)
	in := passingInput(domain.JevDirectionLong)
	in.APIErr = errString(strings.Repeat("障", 400))

	sig := e.Decide(context.Background(), in)

	if sig.RejectReason == nil || len(*sig.RejectReason) > 255 || !strings.HasSuffix(*sig.RejectReason, "…") {
		t.Fatalf("RejectReason = %v, want <= 255 bytes ending with an ellipsis", sig.RejectReason)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
