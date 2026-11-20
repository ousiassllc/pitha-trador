package jevaudit_test

import (
	"context"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset/jevaudit"
)

// Issue #1113: an experiment on a shadow key that was changed on the Settings
// screen is confirmed by switch_check, through the screen's audit row, whether
// the experiment names it by audit_id or the row is found near started_at.
func TestSwitchCheck_ConfirmsScreenOriginatedChange(t *testing.T) {
	for _, tc := range []struct {
		name          string
		key           string
		before, after string
		save          func(ctx context.Context, t *testing.T, svc *jevaudit.Service)
	}{
		{"enabled", jevshadowsettings.KeyEnabled, "false", "true", func(ctx context.Context, t *testing.T, svc *jevaudit.Service) {
			if err := svc.SetEnabled(ctx, true); err != nil {
				t.Fatal(err)
			}
		}},
		{"candidate_version", jevshadowsettings.KeyCandidateVersion, "none", "trader-v4", func(ctx context.Context, t *testing.T, svc *jevaudit.Service) {
			if err := svc.SetCandidate(ctx, "trader-v4"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		for _, byID := range []bool{true, false} {
			name := tc.name + "/nearby row"
			if byID {
				name = tc.name + "/audit_id"
			}
			t.Run(name, func(t *testing.T) {
				ctx := context.Background()
				svc, db := newService(t)
				tc.save(ctx, t, svc)
				var auditID int64
				if err := db.QueryRowContext(ctx, `SELECT id FROM mcp_settings_audit WHERE setting_key = ? AND actor = 'gui'`, tc.key).Scan(&auditID); err != nil {
					t.Fatalf("no screen audit row for %s: %v", tc.key, err)
				}
				req := experiment.Request{
					Hypothesis: "画面から切り替えた",
					Change:     experiment.Change{Kind: experiment.KindSetting, Name: tc.key, Before: tc.before, After: tc.after},
					StartedAt:  savedAt.Add(5 * time.Minute),
				}
				if byID {
					req.AuditID = &auditID
				}
				exp, err := experiment.Insert(ctx, db, savedAt.Add(time.Hour), experiment.ActorGUI, "", req)
				if err != nil {
					t.Fatal(err)
				}
				c, err := experiment.Compare(ctx, db, exp, savedAt.Add(48*time.Hour), 0, 30_000_000, false)
				if err != nil {
					t.Fatal(err)
				}
				if c.Switch.Status != experiment.SwitchConfirmed {
					t.Fatalf("switch_check = %+v, want confirmed", c.Switch)
				}
			})
		}
	}
}
