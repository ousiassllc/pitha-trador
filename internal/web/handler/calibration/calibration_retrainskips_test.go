package calibration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain/metamodel"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

type sourceWithSkips struct {
	calibration.StaticCalibrationSource
	skips []metamodel.RetrainSkip
	err   error
}

func (s sourceWithSkips) RetrainSkips(context.Context) ([]metamodel.RetrainSkip, error) {
	return s.skips, s.err
}

// Issue #799: skipped retrainings are listed (newest first as supplied) with a
// fixed code and the "必要130件中 x件" counts, with no model trained.
func TestCalibrationHandler_APICalibration_MetaModelSkips(t *testing.T) {
	at := time.Date(2026, 10, 19, 18, 0, 0, 0, time.UTC)
	code, raw := getMetaCalibration(t, sourceWithSkips{skips: []metamodel.RetrainSkip{
		{SkippedAt: at, Code: "too_few_rows_before_oos", UsableRows: 87, RequiredRows: 130},
	}})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", code, raw)
	}
	var body struct {
		MetaModel      map[string]any   `json:"meta_model"`
		MetaModelSkips []map[string]any `json:"meta_model_skips"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("Unmarshal: %v (body=%s)", err, raw)
	}
	if body.MetaModel != nil {
		t.Errorf("meta_model = %v, want omitted without a model", body.MetaModel)
	}
	if len(body.MetaModelSkips) != 1 {
		t.Fatalf("meta_model_skips = %v, want one skip", body.MetaModelSkips)
	}
	s := body.MetaModelSkips[0]
	for key, want := range map[string]any{
		"skipped_at": "2026-10-19T18:00:00Z", "reason_code": "too_few_rows_before_oos", "usable_rows": 87.0, "required_rows": 130.0,
	} {
		if s[key] != want {
			t.Errorf("meta_model_skips[0].%s = %v, want %v", key, s[key], want)
		}
	}
}

func TestCalibrationHandler_APICalibration_MetaModelSkipsOmittedWhenEmptyAndSourceError(t *testing.T) {
	_, raw := getMetaCalibration(t, sourceWithSkips{})
	if strings.Contains(string(raw), "meta_model_skips") {
		t.Errorf("body = %s, want meta_model_skips omitted when nothing was skipped", raw)
	}
	if code, _ := getMetaCalibration(t, sourceWithSkips{err: errors.New("db down")}); code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500 when the skips cannot be read", code)
	}
	_, raw = getMetaCalibration(t, calibration.StaticCalibrationSource{})
	if strings.Contains(string(raw), "meta_model_skips") {
		t.Errorf("body = %s, want meta_model_skips omitted without a source", raw)
	}
}
