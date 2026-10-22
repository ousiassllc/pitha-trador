package metalabel

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/ousiassllc/pitha-trador/internal/domain"
)

// Feature names, in the order of Features.Vector. A trained model records
// them (metalabel_models.feature_names_json) and ParseModel refuses a model
// whose names differ, so a reordered or extended vector can never be scored
// by coefficients learned for another layout.
const (
	FeatureOFIShort         = "ofi_short"
	FeatureOFIMid           = "ofi_mid"
	FeatureOFILong          = "ofi_long"
	FeatureMicropriceDevBps = "microprice_dev_bps"
	FeatureSpreadBps        = "spread_bps"
	FeatureReturn1m         = "return_1m"
	FeatureJevScore         = "jev_score"
	FeatureTDnetScore       = "tdnet_score"
)

var featureNames = []string{
	FeatureOFIShort, FeatureOFIMid, FeatureOFILong, FeatureMicropriceDevBps,
	FeatureSpreadBps, FeatureReturn1m, FeatureJevScore, FeatureTDnetScore,
}

// FeatureNames returns the feature names in vector order.
func FeatureNames() []string { return slices.Clone(featureNames) }

// ErrMissingFeature is wrapped by the error Features.Vector returns when a
// feature is nil or not a finite number.
var ErrMissingFeature = errors.New("metalabel: missing feature")

// ErrNoCandidate is returned for a primary signal that is neither LONG nor
// SHORT: there is no candidate to take or skip.
var ErrNoCandidate = errors.New("metalabel: no LONG/SHORT candidate")

// Features is the raw input of the secondary model for one primary signal.
// A nil field means "not available this cycle" (never zero) and makes the
// decision a skip.
//
// OFIShort/OFIMid/OFILong are the order flow imbalance over the three Settings
// windows (default 10/30/60 s, shares), MicropriceDevBps the microprice's
// deviation from the mid, SpreadBps the quoted spread, Return1m the one-minute
// return (a decimal ratio), JevScore jev.Score.Value() and TDnetScore the
// TDnet disclosure score. A symbol with no recent disclosure has TDnetScore 0
// (neutral), which the caller passes explicitly; nil means the score could not
// be computed.
type Features struct {
	OFIShort, OFIMid, OFILong *float64
	MicropriceDevBps          *float64
	SpreadBps                 *float64
	Return1m                  *float64
	JevScore                  *float64
	TDnetScore                *float64
}

// NewFeatures assembles Features from a market snapshot's order-flow features
// and the remaining inputs. jevScore is jev.Score.Value() (nil when the
// decision has no Jev score).
func NewFeatures(orderFlow domain.OrderFlowFeatures, spreadBps, return1m, jevScore, tdnetScore *float64) Features {
	return Features{
		OFIShort: orderFlow.OFIShort, OFIMid: orderFlow.OFIMid, OFILong: orderFlow.OFILong,
		MicropriceDevBps: orderFlow.MicropriceDevBps,
		SpreadBps:        spreadBps, Return1m: return1m, JevScore: jevScore, TDnetScore: tdnetScore,
	}
}

// Vector returns the model input for a primary signal on direction (LONG or
// SHORT), in FeatureNames order. The direction-dependent features (the three
// OFIs, the microprice deviation and the 1-minute return) are signed towards
// the candidate side: a SHORT negates them, so a positive value always means
// "the market is moving the candidate's way" and one model serves both sides.
func (f Features) Vector(direction string) ([]float64, error) {
	var sign float64
	switch direction {
	case domain.JevDirectionLong:
		sign = 1
	case domain.JevDirectionShort:
		sign = -1
	default:
		return nil, fmt.Errorf("%w: direction %q", ErrNoCandidate, direction)
	}
	inputs := f.inputs()
	v := make([]float64, len(inputs))
	for i, in := range inputs {
		if !in.present() {
			return nil, fmt.Errorf("%w: %s", ErrMissingFeature, in.name)
		}
		v[i] = *in.value
		if in.oriented {
			v[i] *= sign
		}
	}
	return v, nil
}

// featureInput is one model input: its name, raw value and whether Vector
// signs it towards the candidate side.
type featureInput struct {
	name     string
	value    *float64
	oriented bool
}

// present reports whether the input is a finite number.
func (in featureInput) present() bool {
	return in.value != nil && !math.IsNaN(*in.value) && !math.IsInf(*in.value, 0)
}

// inputs lists the model inputs in FeatureNames order.
func (f Features) inputs() []featureInput {
	return []featureInput{
		{FeatureOFIShort, f.OFIShort, true},
		{FeatureOFIMid, f.OFIMid, true},
		{FeatureOFILong, f.OFILong, true},
		{FeatureMicropriceDevBps, f.MicropriceDevBps, true},
		{FeatureSpreadBps, f.SpreadBps, false},
		{FeatureReturn1m, f.Return1m, true},
		{FeatureJevScore, f.JevScore, false},
		{FeatureTDnetScore, f.TDnetScore, false},
	}
}

// MissingNames returns every feature that is nil or not a finite number, in
// FeatureNames order: exactly the features that make Vector fail for a LONG or
// SHORT signal (Vector reports only the first one).
func (f Features) MissingNames() []string {
	var missing []string
	for _, in := range f.inputs() {
		if !in.present() {
			missing = append(missing, in.name)
		}
	}
	return missing
}

// JSON returns the features as the object stored in
// metalabel_predictions.features_json (name -> raw value, null when missing),
// so a prediction can be audited and reproduced.
func (f Features) JSON() string {
	b, err := json.Marshal(map[string]*float64{
		FeatureOFIShort: f.OFIShort, FeatureOFIMid: f.OFIMid, FeatureOFILong: f.OFILong,
		FeatureMicropriceDevBps: f.MicropriceDevBps, FeatureSpreadBps: f.SpreadBps,
		FeatureReturn1m: f.Return1m, FeatureJevScore: f.JevScore, FeatureTDnetScore: f.TDnetScore,
	})
	if err != nil {
		// A NaN/Inf raw value is not representable in JSON; the prediction is a
		// skip in that case (Vector rejects it), so record an empty object.
		return "{}"
	}
	return string(b)
}
