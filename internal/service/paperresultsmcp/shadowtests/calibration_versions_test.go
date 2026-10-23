package shadowtests

import (
	"net/http"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

type calibrationVersions struct {
	TraderByVersion []struct {
		PromptVersion string `json:"prompt_version"`
		TraderCount   int    `json:"trader_count"`
	} `json:"trader_decisions_by_version"`
	PromptVersions struct {
		Only     string   `json:"only"`
		Excluded []string `json:"excluded"`
	} `json:"prompt_versions"`
	Horizons []struct {
		ByLabelVersion []struct {
			SampleCount int      `json:"sample_count"`
			HitRate     *float64 `json:"hit_rate"`
		} `json:"by_label_version"`
	} `json:"horizons"`
}

// Issue #881: jev_calibration does not blend prompt versions. Two decisions of
// trader-v3 (one right, one wrong) and one of trader-v4 (right) are labeled at
// the same horizon. By default only the live version's cohort is graded
// (trader-v3 until a promotion stores another), prompt_version grades one exact
// version, and after a promotion the default follows the new live version. The
// decision counts of every version stay visible per version.
func TestJevCalibrationDoesNotMixPromptVersions(t *testing.T) {
	srv, w := shadowServer(t)
	for id, version := range map[int]string{1: "trader-v3", 2: "trader-v3", 3: "trader-v4"} {
		liveDecision(t, w, id, "2026-10-01T01:00:00Z", version, "LONG", "good")
		outcome(t, w, id, 5, 1, 0.1, id != 2, nil, nil)
	}

	call := func(t *testing.T, extra map[string]any) (samples int, d calibrationVersions) {
		t.Helper()
		args := map[string]any{"from": period["from"], "to": period["to"]}
		for k, v := range extra {
			args[k] = v
		}
		srv.call(t, "jev_calibration", args).data(t, "jev_calibration", &d)
		for _, h := range d.Horizons {
			for _, v := range h.ByLabelVersion {
				samples += v.SampleCount
			}
		}
		return samples, d
	}

	t.Run("default is the live version's cohort", func(t *testing.T) {
		n, d := call(t, nil)
		if n != 2 || !slices.Equal(d.PromptVersions.Excluded, []string{"trader-v4"}) {
			t.Fatalf("graded %d samples, excluded %v; want the 2 trader-v3 samples, trader-v4 excluded", n, d.PromptVersions.Excluded)
		}
		if len(d.TraderByVersion) != 2 || d.TraderByVersion[0].PromptVersion != "trader-v3" || d.TraderByVersion[0].TraderCount != 2 ||
			d.TraderByVersion[1].PromptVersion != "trader-v4" || d.TraderByVersion[1].TraderCount != 1 {
			t.Fatalf("trader_decisions_by_version = %+v, want trader-v3 2 and trader-v4 1", d.TraderByVersion)
		}
		approx(t, "hit_rate", d.Horizons[0].ByLabelVersion[0].HitRate, 0.5)
	})
	t.Run("prompt_version grades that version only", func(t *testing.T) {
		n, d := call(t, map[string]any{"prompt_version": "trader-v4"})
		if n != 1 || d.PromptVersions.Only != "trader-v4" || len(d.PromptVersions.Excluded) != 0 {
			t.Fatalf("graded %d samples, scope %+v; want the 1 trader-v4 sample", n, d.PromptVersions)
		}
		approx(t, "hit_rate", d.Horizons[0].ByLabelVersion[0].HitRate, 1)
	})
	t.Run("an invalid prompt_version is rejected", func(t *testing.T) {
		srv.call(t, "jev_calibration", map[string]any{"prompt_version": "x y"}).
			wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	})

	testkit.Setting(t, w, jevshadowsettings.KeyLiveVersion, `"trader-v4"`)
	t.Run("after a promotion the default follows the new live version", func(t *testing.T) {
		n, d := call(t, nil)
		if n != 1 || !slices.Equal(d.PromptVersions.Excluded, []string{"trader-v1", "trader-v2", "trader-v3"}) {
			t.Fatalf("graded %d samples, excluded %v; want the 1 trader-v4 sample", n, d.PromptVersions.Excluded)
		}
	})
}
