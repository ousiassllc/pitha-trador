package opsflow_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/barriersettings"
	"github.com/ousiassllc/pitha-trador/internal/config/costsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/exitalign"
)

func costLine(t *testing.T, body, group, key string) string {
	t.Helper()
	marker := `data-testid="setting-group-derived-` + group + `-` + key + `"`
	_, rest, ok := strings.Cut(body, marker)
	if !ok {
		t.Fatalf("no cost panel line %s/%s; body=%s", group, key, body)
	}
	line, _, _ := strings.Cut(rest, "</li>")
	return line
}

// issue #779: with the shipped defaults the cost and triple-barrier groups both
// say the label cost and the fill model cost agree; no warning line renders.
func TestSettingsPage_CostPanelAlignedAtDefaults(t *testing.T) {
	engine, _ := opsRouter()
	body := allPagesBody(t, engine)
	for _, group := range []string{"trading_cost", "triple_barrier"} {
		if line := costLine(t, body, group, "aligned"); !strings.Contains(line, "整合しています") {
			t.Errorf("%s aligned line = %s", group, line)
		}
		if strings.Contains(body, "setting-group-derived-"+group+"-warn-") {
			t.Errorf("%s warns at the defaults", group)
		}
	}
}

// A stored base cost far from the default warns in both groups.
func TestSettingsPage_CostPanelWarnsOnBaseCostMismatch(t *testing.T) {
	engine, store := opsRouter()
	store.rows[barriersettings.KeyCostBps] = "40"
	body := allPagesBody(t, engine)
	for _, group := range []string{"trading_cost", "triple_barrier"} {
		line := costLine(t, body, group, "warn-"+exitalign.WarnCost)
		if !strings.Contains(line, "注意:") || !strings.Contains(line, "calibration.barrier_cost_bps") {
			t.Errorf("%s mismatch warning = %s", group, line)
		}
	}
}

// Saving a cost value refreshes the cost panel of the other group
// out-of-band, and the answer carries the new label cost.
func TestSaveOps_CostSaveRefreshesBothCostPanels(t *testing.T) {
	engine, _ := opsRouter()
	rec := opsRequest(engine, http.MethodPost, costsettings.KeyFeeBps, url.Values{"value": {"5"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, group := range []string{"trading_cost", "triple_barrier"} {
		// 2 bps per fill fee x2 = +10 bps over the 4 bps round trip.
		if line := costLine(t, body, group, "long"); !strings.Contains(line, "ラベル 20 bps ／ 約定モデル 14 bps") {
			t.Errorf("%s LONG line after saving fee 5 = %s", group, line)
		}
	}

	engine, _ = opsRouter()
	rec = opsRequest(engine, http.MethodPost, barriersettings.KeyCostBps, url.Values{"value": {"25"}}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if line := costLine(t, rec.Body.String(), "trading_cost", "warn-"+exitalign.WarnCost); !strings.Contains(line, "注意:") {
		t.Errorf("saving a barrier cost of 25 does not refresh the cost group's warning: %s", line)
	}
}
