package system_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
)

func countHistory(t *testing.T, db *sql.DB, where string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_role_history WHERE ` + where).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Saving a role writes the setting and its history row together, from the setting
// in force before (the default while nothing is stored) to the saved one.
func TestAIRoleRepository_SaveRecordsTheSwitchWithTheSetting(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewAIRoleRepository(db)
	ctx := context.Background()
	repo.SetClock(func() time.Time { return time.Date(2026, 11, 15, 1, 2, 3, 0, time.UTC) })

	or := airole.Setting{Provider: airole.ProviderOpenRouter, Model: "vendor/model"}
	if err := repo.Save(ctx, airole.RoleJev, or); err != nil {
		t.Fatal(err)
	}
	stored, ok, err := system.NewRuntimeSettingsRepository(db).Get(ctx, airole.Key(airole.RoleJev))
	if err != nil || !ok || stored != `{"provider":"openrouter","model":"vendor/model"}` {
		t.Fatalf("stored = %q ok=%v err=%v", stored, ok, err)
	}
	rows, err := repo.History(ctx, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("history = %+v, %v", rows, err)
	}
	r := rows[0]
	if r.Kind != system.AIRoleSaved || r.Role != "jev" || r.Before != airole.DefaultSetting() || r.After != or || !r.At.Equal(time.Date(2026, 11, 15, 1, 2, 3, 0, time.UTC)) {
		t.Errorf("row = %+v", r)
	}

	// The same value again, and a reset to the default, are handled as switches only when they change something.
	if err := repo.Save(ctx, airole.RoleJev, or); err != nil {
		t.Fatal(err)
	}
	if n := countHistory(t, db, `kind = 'saved'`); n != 1 {
		t.Errorf("saved rows after an unchanged save = %d, want 1", n)
	}
	if err := repo.Reset(ctx, airole.RoleJev); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := system.NewRuntimeSettingsRepository(db).Get(ctx, airole.Key(airole.RoleJev)); ok {
		t.Error("the setting is still stored after Reset")
	}
	rows, _ = repo.History(ctx, 10)
	if len(rows) != 2 || rows[0].Before != or || rows[0].After != airole.DefaultSetting() {
		t.Errorf("history after Reset = %+v", rows)
	}
	if err := repo.Reset(ctx, airole.RoleJev); err != nil || countHistory(t, db, `kind = 'saved'`) != 2 {
		t.Errorf("a second Reset recorded a row (err=%v)", err)
	}
}

// A damaged stored value must stay replaceable, and counts as the default before the save.
func TestAIRoleRepository_DamagedValueCanBeReplaced(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	if err := system.NewRuntimeSettingsRepository(db).Set(ctx, airole.Key("sol"), "garbage", time.Now()); err != nil {
		t.Fatal(err)
	}
	repo := system.NewAIRoleRepository(db)
	if err := repo.Save(ctx, "sol", airole.Setting{Provider: "openrouter", Model: "a/b"}); err != nil {
		t.Fatal(err)
	}
	rows, _ := repo.History(ctx, 5)
	if len(rows) != 1 || rows[0].Before != airole.DefaultSetting() {
		t.Errorf("history = %+v", rows)
	}
}

func TestAIRoleRepository_SaveRefusesAnInvalidSettingAndWritesNothing(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewAIRoleRepository(db)
	for _, bad := range []airole.Setting{{Provider: "openrouter"}, {Provider: "x"}} {
		if err := repo.Save(context.Background(), "jev", bad); err == nil {
			t.Errorf("Save(%+v) succeeded", bad)
		}
	}
	if err := repo.Save(context.Background(), "nobody", airole.DefaultSetting()); err == nil {
		t.Error("Save of an unknown role succeeded")
	}
	var n int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM ai_role_history) + (SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'ai.role.%')`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows after refused saves = %d (%v)", n, err)
	}
}

// RecordStarted writes a row only when the pair changed since the last start record.
func TestAIRoleRepository_RecordStartedWritesOnlyChanges(t *testing.T) {
	db := newTestDB(t)
	repo := system.NewAIRoleRepository(db)
	ctx := context.Background()
	custom := airole.Setting{Provider: "custom", Model: "jev-latest"}
	or := airole.Setting{Provider: "openrouter", Model: "a/b"}
	for i, tc := range []struct {
		set  airole.Setting
		want bool
	}{{custom, true}, {custom, false}, {or, true}, {or, false}, {custom, true}} {
		wrote, err := repo.RecordStarted(ctx, "jev", tc.set)
		if err != nil || wrote != tc.want {
			t.Fatalf("step %d: wrote=%v err=%v, want %v", i, wrote, err, tc.want)
		}
	}
	last, ok, err := repo.LastStarted(ctx, "jev")
	if err != nil || !ok || last.After != custom || last.Before != (airole.Setting{}) {
		t.Errorf("LastStarted = %+v ok=%v err=%v", last, ok, err)
	}
	if _, ok, _ := repo.LastStarted(ctx, "sol"); ok {
		t.Error("sol has a start record")
	}
}

// Only provider and model names reach the table: its columns cannot hold a key or URL by design,
// and the schema refuses an unknown provider, a saved row without a before, and an unchanged save.
func TestAIRoleHistory_SchemaConstraints(t *testing.T) {
	db := newTestDB(t)
	insert := func(kind, actor, role string, bp, bm any, provider, model string) error {
		_, err := db.Exec(`INSERT INTO ai_role_history (at, kind, actor, role, before_provider, before_model, provider, model) VALUES ('2026-11-15T00:00:00Z', ?, ?, ?, ?, ?, ?, ?)`,
			kind, actor, role, bp, bm, provider, model)
		return err
	}
	if err := insert("saved", "gui", "jev", "custom", "", "openrouter", "a/b"); err != nil {
		t.Fatalf("valid saved row: %v", err)
	}
	if err := insert("started", "app", "sol", nil, nil, "custom", ""); err != nil {
		t.Fatalf("valid started row: %v", err)
	}
	for name, err := range map[string]error{
		"mcp as an actor":       insert("saved", "mcp", "jev", "custom", "", "openrouter", "a/b"),
		"unknown role":          insert("saved", "gui", "scout", "custom", "", "openrouter", "a/b"),
		"unknown provider":      insert("saved", "gui", "jev", "custom", "", "chatgpt", "a/b"),
		"saved without before":  insert("saved", "gui", "jev", nil, nil, "openrouter", "a/b"),
		"unchanged save":        insert("saved", "gui", "jev", "custom", "m", "custom", "m"),
		"started with a before": insert("started", "app", "jev", "custom", "", "custom", ""),
		"started by the gui":    insert("started", "gui", "jev", nil, nil, "custom", ""),
	} {
		if err == nil {
			t.Errorf("%s was accepted", name)
		} else if !strings.Contains(err.Error(), "constraint") {
			t.Errorf("%s: unexpected error %v", name, err)
		}
	}
}
