package shadowtests

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query/shadowquery"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

func TestCostShowsTodayTotalDailyAndTheUnapprovedProposedCap(t *testing.T) {
	srv, w := shadowServer(t) // the fixed clock is 2026-10-18T03:00Z = 2026-10-18 12:00 JST
	liveDecision(t, w, 1, "2026-10-18T01:00:00Z", "trader-v3", "LONG", "poor")
	// Yesterday 23:30 JST (14:30Z on the 17th) and today 00:30 JST (15:30Z on the 17th is JST 10-18 00:30).
	shadowDecision(t, w, 1, "trader-v4", "LONG", "good", 0.25, "2026-10-17T14:30:00Z")
	shadowDecision(t, w, 1, "trader-v5", "LONG", "good", 0.5, "2026-10-17T15:30:00Z")
	shadowDecision(t, w, 1, "trader-v6", "LONG", "good", 0.125, "2026-10-18T02:00:00Z")

	var c shadowquery.ShadowCostStatus
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &c)
	if c.Today.Date != "2026-10-18" || c.Today.Calls != 2 || c.Today.RequestCostUSD != 0.625 {
		t.Errorf("today = %+v, want 2026-10-18 / 2 calls / 0.625 (JST day boundary)", c.Today)
	}
	if c.Total.Calls != 3 || c.Total.RequestCostUSD != 0.875 {
		t.Errorf("total = %+v, want 3 calls / 0.875", c.Total)
	}
	if len(c.Daily) != 2 || c.Daily[0].Date != "2026-10-18" || c.Daily[1].Date != "2026-10-17" || c.Daily[1].RequestCostUSD != 0.25 {
		t.Errorf("daily = %+v", c.Daily)
	}
	// Nothing stored: the shadow is off, no candidate, the live version is the build default, and the cap
	// is the unapproved proposal.
	if c.Enabled || c.CandidateVersion != nil || c.LiveVersion != "trader-v3" {
		t.Errorf("state = enabled %v candidate %v live %q", c.Enabled, c.CandidateVersion, c.LiveVersion)
	}
	if c.DailyCapUSD != jevshadowsettings.ProposedDailyCostCapUSD || !c.CapIsDefault || c.CapSource != shadowquery.CapSourceProposed {
		t.Errorf("cap = %v default %v source %q, want the unapproved proposal", c.DailyCapUSD, c.CapIsDefault, c.CapSource)
	}
	if c.FailureCap != jevshadowsettings.ProposedDailyFailureCap || c.FailureCapSource != shadowquery.CapSourceProposed || c.FailuresToday != 0 {
		t.Errorf("failure cap = %d (%q) today %d, want the unapproved proposal and 0", c.FailureCap, c.FailureCapSource, c.FailuresToday)
	}
	if c.HaltToday != nil || c.LastHalt != nil {
		t.Errorf("halt = %+v / %+v, want none", c.HaltToday, c.LastHalt)
	}
	if len(c.RegisteredVersions) < 2 {
		t.Errorf("registered versions = %v", c.RegisteredVersions)
	}

	// The stored switch, candidate, cap and a halt record are read back.
	testkit.Setting(t, w, jevshadowsettings.KeyEnabled, "1")
	testkit.Setting(t, w, jevshadowsettings.KeyCandidateVersion, `"trader-v4"`)
	testkit.Setting(t, w, jevshadowsettings.KeyDailyCostCapUSD, "0.5")
	testkit.Setting(t, w, jevshadowsettings.KeyHalt, `{"date":"2026-10-18","reason":"daily_cost_cap","spent_usd":0.625,"cap_usd":0.5,"at":"2026-10-18T02:00:00Z"}`)
	testkit.Setting(t, w, jevshadowsettings.KeyFailures, `{"date":"2026-10-18","count":7}`)
	var s shadowquery.ShadowCostStatus
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &s)
	if !s.Enabled || s.CandidateVersion == nil || *s.CandidateVersion != "trader-v4" || s.DailyCapUSD != 0.5 || s.CapIsDefault || s.CapSource != shadowquery.CapSourceStored {
		t.Errorf("state = %+v", s)
	}
	if s.HaltToday == nil || s.HaltToday.Reason != "daily_cost_cap" || s.HaltToday.SpentUSD != 0.625 || s.LastHalt == nil {
		t.Errorf("halt = %+v / %+v, want the daily_cost_cap stop of today", s.HaltToday, s.LastHalt)
	}
}

func TestPromoteSwitchesTheLiveVersionAndLeavesAnAuditRow(t *testing.T) {
	srv, w := shadowServer(t)
	var p struct {
		PromotionID int64  `json:"promotion_id"`
		From        string `json:"from_version"`
		To          string `json:"to_version"`
		At          string `json:"at"`
	}
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v4"}).data(t, "jev_prompt_promote", &p)
	if p.PromotionID == 0 || p.From != "trader-v3" || p.To != "trader-v4" || p.At == "" {
		t.Fatalf("promote = %+v", p)
	}
	if v, ok := storedValue(t, w, jevshadowsettings.KeyLiveVersion); !ok || v != `"trader-v4"` {
		t.Fatalf("stored live version = %q, %v", v, ok)
	}
	var actor, client, from, to string
	if err := w.QueryRow(`SELECT actor, client, from_version, to_version FROM jev_prompt_promotions`).Scan(&actor, &client, &from, &to); err != nil {
		t.Fatal(err)
	}
	if actor != "mcp" || client == "" || strings.ContainsAny(client, " ") || from != "trader-v3" || to != "trader-v4" {
		t.Fatalf("audit row = %s %q %s %s: who (mcp + host), previous and new version", actor, client, from, to)
	}

	// jev_shadow_cost shows the promotion and the new live version.
	var c shadowquery.ShadowCostStatus
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &c)
	if c.LiveVersion != "trader-v4" || len(c.RecentPromotions) != 1 || c.RecentPromotions[0].FromVersion != "trader-v3" || c.RecentPromotions[0].Actor != "mcp" {
		t.Fatalf("after promotion: live %q promotions %+v", c.LiveVersion, c.RecentPromotions)
	}

	// The shadow's own settings are untouched: a promotion does not enable the shadow.
	for _, key := range []string{jevshadowsettings.KeyEnabled, jevshadowsettings.KeyCandidateVersion, jevshadowsettings.KeyDailyCostCapUSD} {
		if _, ok := storedValue(t, w, key); ok {
			t.Errorf("a promotion stored %s", key)
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM mcp_settings_audit`); n != 0 {
		t.Errorf("mcp_settings_audit rows = %d: promotions have their own audit table", n)
	}

	// Promoting the now-live version again, or an unknown one, is refused and writes nothing.
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v4"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v999"}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidValue)
	srv.call(t, "jev_prompt_promote", map[string]any{"version": ""}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "jev_prompt_promote", map[string]any{}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v3", "extra": 1}).wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
	if n := count(t, w, `SELECT COUNT(*) FROM jev_prompt_promotions`); n != 1 {
		t.Errorf("promotion rows = %d, want 1 (refused promotions leave nothing)", n)
	}
}

// The promotion follows the write-permission switch of settings_set (§7.2).
func TestPromoteFollowsTheWriteSwitch(t *testing.T) {
	srv, w := shadowServer(t)
	srv.call(t, "settings_write_disable", map[string]any{}).data(t, "settings_write_disable", &struct{}{})
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v4"}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if _, ok := storedValue(t, w, jevshadowsettings.KeyLiveVersion); ok {
		t.Fatal("a refused promotion changed the live version")
	}
	if n := count(t, w, `SELECT COUNT(*) FROM jev_prompt_promotions`); n != 0 {
		t.Fatalf("a refused promotion left %d audit rows", n)
	}
	srv.call(t, "settings_write_enable", map[string]any{}).data(t, "settings_write_enable", &struct{}{})
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v4"}).data(t, "jev_prompt_promote", &struct{}{})
	if v, _ := storedValue(t, w, jevshadowsettings.KeyLiveVersion); v != `"trader-v4"` {
		t.Fatalf("live version = %q after the switch is on", v)
	}
}

// The MCP can neither turn the shadow on nor pick the candidate nor set the
// cost cap: those keys are not on the settings_set whitelist.
func TestShadowSettingsCannotBeWrittenThroughSettingsSet(t *testing.T) {
	srv, w := shadowServer(t)
	for _, key := range jevshadowsettings.Keys() {
		srv.call(t, "settings_set", map[string]any{"key": key, "value": "1"}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeKeyNotAllowed)
		if _, ok := storedValue(t, w, key); ok {
			t.Errorf("settings_set stored %s", key)
		}
	}
}

// The write path stays narrow: running the promotion and the reads touches
// only runtime_settings (the live-version upsert) and jev_prompt_promotions, and
// the reads never write, read secrets, or return the raw state/response JSON.
func TestPromotionWritePathIsNarrowAndTheReadsAreReadOnly(t *testing.T) {
	path, w := testkit.NewMigratedDB(t)
	liveDecision(t, w, 1, "2026-10-01T01:00:00Z", "trader-v3", "LONG", "poor")
	shadowDecision(t, w, 1, "trader-v4", "LONG", "good", 0.0001, "2026-10-01T01:00:05Z")
	outcome(t, w, 1, 5, 2, 1.0, 1, "tp", 5.0)
	driverName, counter := testkit.NewCountingDriver(t)
	srv := newServer(t, paperresultsmcp.Options{Settings: enabledSettings(), DBPath: path, DriverName: driverName})

	srv.call(t, "jev_shadow_compare", period).data(t, "jev_shadow_compare", &struct{}{})
	srv.call(t, "jev_shadow_cost", map[string]any{}).data(t, "jev_shadow_cost", &struct{}{})
	srv.call(t, "jev_prompt_promote", map[string]any{"version": "trader-v4"}).data(t, "jev_prompt_promote", &struct{}{})

	var writes int
	for _, stmt := range counter.StatementLog() {
		s := strings.ToUpper(strings.TrimSpace(stmt))
		isWrite := strings.HasPrefix(s, "INSERT") || strings.HasPrefix(s, "UPDATE") || strings.HasPrefix(s, "DELETE") ||
			strings.HasPrefix(s, "CREATE") || strings.HasPrefix(s, "DROP") || strings.HasPrefix(s, "ALTER")
		if !isWrite {
			for _, forbidden := range []string{"SECRETS", "SELECT *", "STATE_JSON", "RAW_DATA_JSON", ".*"} {
				if strings.Contains(s, forbidden) {
					t.Errorf("a read mentions %q: %s", forbidden, testkit.FirstLine(stmt))
				}
			}
			if strings.Contains(s, "RESPONSE_JSON") && !strings.Contains(s, "JSON_EXTRACT(D.RESPONSE_JSON, '$.ENTRY_QUALITY')") {
				t.Errorf("response_json is read other than as the single entry_quality scalar: %s", testkit.FirstLine(stmt))
			}
			continue
		}
		writes++
		switch {
		case strings.HasPrefix(s, "INSERT INTO RUNTIME_SETTINGS"):
		case strings.HasPrefix(s, "INSERT INTO JEV_PROMPT_PROMOTIONS"):
		default:
			t.Errorf("a write statement outside the promotion's two: %s", testkit.FirstLine(stmt))
		}
	}
	if writes != 2 {
		t.Errorf("write statements = %d, want exactly 2 (live-version upsert and the promotion row)", writes)
	}
	db, err := srv.DB()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO jev_shadow_decisions (jev_decision_id, prompt_version, model_id, response_json, direction, regime, entry_quality, confidence, toxic_flow, liquidity_stressed, continuation_probability, latency_ms) VALUES (1,'x','m','{}','LONG','TREND','good',0.5,0,0,0,1)`,
		`INSERT INTO jev_prompt_promotions (at, actor, client, from_version, to_version) VALUES ('t','mcp','','a','b')`,
		`UPDATE jev_shadow_decisions SET request_cost = 0`,
	} {
		if _, err := db.Exec(q); err == nil {
			t.Errorf("the read connection accepted a write: %s", testkit.FirstLine(q))
		}
	}
}
