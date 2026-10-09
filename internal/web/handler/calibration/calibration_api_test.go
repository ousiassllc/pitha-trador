package calibration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/calibration"
)

// horizonSource records the horizon the handler asked for.
type horizonSource struct{ got []int }

func (s *horizonSource) Metrics(_ context.Context, horizonMinutes int) (domain.CalibrationMetrics, error) {
	s.got = append(s.got, horizonMinutes)
	return domain.CalibrationMetrics{}, nil
}

func getCalibration(t *testing.T, source *horizonSource, query string) (int, string) {
	t.Helper()
	_, api := humatest.New(t)
	huma.Get(api, "/api/v1/calibration", calibration.NewCalibrationHandler(source).APICalibration)
	resp := api.Get("/api/v1/calibration" + query)
	var body struct {
		Horizon string `json:"horizon"`
	}
	if resp.Code == http.StatusOK {
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("Unmarshal: %v (body=%s)", err, resp.Body.String())
		}
	}
	return resp.Code, body.Horizon
}

// Issue #719: `horizon` selects one judgment horizon (5/10/15) or all (the
// default, 0 for the source), and the response echoes it.
func TestCalibrationHandler_APICalibration_HorizonQuery(t *testing.T) {
	for _, tc := range []struct {
		query       string
		wantMinutes int
		wantHorizon string
	}{
		{"", 0, "all"},
		{"?horizon=all", 0, "all"},
		{"?horizon=5", 5, "5"},
		{"?horizon=10", 10, "10"},
		{"?horizon=15", 15, "15"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			source := &horizonSource{}
			code, horizon := getCalibration(t, source, tc.query)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want %d", code, http.StatusOK)
			}
			if len(source.got) != 1 || source.got[0] != tc.wantMinutes {
				t.Fatalf("source horizons = %v, want [%d]", source.got, tc.wantMinutes)
			}
			if horizon != tc.wantHorizon {
				t.Fatalf("response horizon = %q, want %q", horizon, tc.wantHorizon)
			}
		})
	}
}

func TestCalibrationHandler_APICalibration_RejectsUnsupportedHorizon(t *testing.T) {
	for _, query := range []string{"?horizon=20", "?horizon=abc", "?horizon=0"} {
		source := &horizonSource{}
		code, _ := getCalibration(t, source, query)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want %d", query, code, http.StatusUnprocessableEntity)
		}
		if len(source.got) != 0 {
			t.Errorf("%s: source was called with %v, want no call", query, source.got)
		}
	}
}
