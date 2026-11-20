package trial

import (
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
	"github.com/ousiassllc/pitha-trador/internal/domain/jevtrial"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/systemone"
	"github.com/ousiassllc/pitha-trador/internal/service/jev/trialwire"
)

// The replay evaluation (issue #1085, internal/service/jevreplay) prices,
// stores and redacts an arm's call exactly as the trial does. These are thin
// pure exports of what is already here; they change nothing the trial does.

// ProviderFor is the routing preference a call to arm carries: data collection
// denied unless the arm allows it, and, when it names providers, only those
// with fallbacks off.
func ProviderFor(arm jevtrialsettings.Arm) *systemone.Provider { return providerFor(arm) }

// CallOutcome is the outcome of one arm call as BuildDecision takes it: the
// response (zero when Err is set), the latency, the request body's size and the
// error (nil for an answer).
type CallOutcome struct {
	Response     trialwire.Response
	Latency      time.Duration
	RequestBytes int
	Err          error
}

// BuildDecision turns one arm call's outcome into the row the trial would store
// for live decision liveID: status, answers, failure classification, the
// credential-redacted error excerpt (secrets: the configured key strings) and
// the cost it is counted at. stateBytes is the size of the input sent. The row's
// LLMCutoffStatus is CutoffUnverified; a caller that knows better sets it.
func BuildDecision(arm jevtrialsettings.Arm, liveID int64, stateBytes int, out CallOutcome, now time.Time, secrets []string) jevtrial.Decision {
	return buildDecision(arm, armInput{live: liveID, stateBytes: stateBytes},
		callResult{resp: out.Response, latency: out.Latency, requestBytes: out.RequestBytes, err: out.Err}, now, secrets)
}

// ReservedUSD is what a call of requestBytes is reserved at against the daily
// caps: the estimated input tokens at the worst-case price (see reservedUSD).
func ReservedUSD(requestBytes int) float64 { return reservedUSD(requestBytes) }
