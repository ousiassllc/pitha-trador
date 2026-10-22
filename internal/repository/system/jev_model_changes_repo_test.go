package system_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

type modelChange struct {
	at, actor     string
	before, after *string
}

func readModelChanges(t *testing.T, db *sql.DB) []modelChange {
	t.Helper()
	rows, err := db.Query(`SELECT at, actor, before_value, after_value FROM jev_model_changes ORDER BY id`)
	if err != nil {
		t.Fatalf("query jev_model_changes: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []modelChange
	for rows.Next() {
		var c modelChange
		if err := rows.Scan(&c.at, &c.actor, &c.before, &c.after); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func str(c *string) string {
	if c == nil {
		return "<NULL>"
	}
	return *c
}

// issue #885: switching JEV_MODEL records when, from what, to what and by whom.
func TestSecretsRepository_SetJevModel_RecordsTheSwitch(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewSecretsRepository(db)
	ctx := context.Background()

	start := time.Now().UTC().Add(-time.Second)
	if err := repo.Set(ctx, config.KeyJevModel, "jev-1.12.0"); err != nil {
		t.Fatalf("first Set: %v", err)
	}
	if err := repo.Set(ctx, config.KeyJevModel, "jev-1.13.0"); err != nil {
		t.Fatalf("second Set: %v", err)
	}
	end := time.Now().UTC().Add(time.Second)

	got := readModelChanges(t, db)
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2: %+v", len(got), got)
	}
	for i, want := range [][2]string{{"<NULL>", "jev-1.12.0"}, {"jev-1.12.0", "jev-1.13.0"}} {
		if str(got[i].before) != want[0] || str(got[i].after) != want[1] {
			t.Errorf("row %d = %s -> %s, want %s -> %s", i, str(got[i].before), str(got[i].after), want[0], want[1])
		}
		if got[i].actor != "gui" {
			t.Errorf("row %d actor = %q, want gui", i, got[i].actor)
		}
		at, err := time.Parse(time.RFC3339, got[i].at)
		if err != nil || at.Before(start) || at.After(end) {
			t.Errorf("row %d at = %q (%v), want a time between %v and %v", i, got[i].at, err, start, end)
		}
	}
	if value, _, _ := repo.Get(ctx, config.KeyJevModel); value != "jev-1.13.0" {
		t.Errorf("stored JEV_MODEL = %q, want jev-1.13.0", value)
	}
}

// Saving the same model again is not a switch; deleting an unset model is not
// either; deleting a set one records the switch back to the default (NULL).
func TestSecretsRepository_JevModel_OnlyRealSwitchesAreRecorded(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewSecretsRepository(db)
	ctx := context.Background()

	if err := repo.Delete(ctx, config.KeyJevModel); err != nil {
		t.Fatalf("Delete of an unset model: %v", err)
	}
	if err := repo.Set(ctx, config.KeyJevModel, "m1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := repo.Set(ctx, config.KeyJevModel, "m1"); err != nil {
		t.Fatalf("Set again: %v", err)
	}
	if err := repo.Delete(ctx, config.KeyJevModel); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	got := readModelChanges(t, db)
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2 (set, delete): %+v", len(got), got)
	}
	if str(got[0].before) != "<NULL>" || str(got[0].after) != "m1" || str(got[1].before) != "m1" || str(got[1].after) != "<NULL>" {
		t.Errorf("rows = %s->%s, %s->%s; want <NULL>->m1, m1-><NULL>", str(got[0].before), str(got[0].after), str(got[1].before), str(got[1].after))
	}
	if _, ok, _ := repo.Get(ctx, config.KeyJevModel); ok {
		t.Error("JEV_MODEL still stored after Delete")
	}
}

// issue #885: no secret value other than the model name reaches the record,
// and the other keys leave it untouched.
func TestSecretsRepository_JevModelChanges_NeverHoldOtherSecrets(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewSecretsRepository(db)
	ctx := context.Background()

	others := map[string]string{
		config.KeyJevAPIKey:       "sk-jev-api-key-value",
		config.KeyJevBaseURL:      "https://jev.example.com",
		config.KeyKabuAPIPassword: "kabu-password-value",
		config.KeySlackWebhookURL: "https://hooks.example/webhook-value",
		config.KeyLunaAPIKey:      "luna-api-key-value",
	}
	for key, value := range others {
		if err := repo.Set(ctx, key, value); err != nil {
			t.Fatalf("Set %s: %v", key, err)
		}
		if err := repo.Set(ctx, key, value+"-2"); err != nil {
			t.Fatalf("Set %s again: %v", key, err)
		}
	}
	if n := len(readModelChanges(t, db)); n != 0 {
		t.Fatalf("rows after saving other keys = %d, want 0", n)
	}

	if err := repo.Set(ctx, config.KeyJevModel, "jev-1.13.0"); err != nil {
		t.Fatalf("Set model: %v", err)
	}
	if err := repo.Delete(ctx, config.KeyKabuAPIPassword); err != nil {
		t.Fatalf("Delete other key: %v", err)
	}
	var dump strings.Builder
	for _, c := range readModelChanges(t, db) {
		dump.WriteString(c.at + "|" + c.actor + "|" + str(c.before) + "|" + str(c.after) + "\n")
	}
	if n := len(readModelChanges(t, db)); n != 1 {
		t.Fatalf("rows = %d, want 1 (only the model switch)", n)
	}
	for _, value := range others {
		if strings.Contains(dump.String(), value) {
			t.Errorf("jev_model_changes contains the secret %q:\n%s", value, dump.String())
		}
	}
}
