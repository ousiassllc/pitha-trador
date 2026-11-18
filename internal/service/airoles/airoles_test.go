package airoles_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/airole"
	"github.com/ousiassllc/pitha-trador/internal/repository/sqlitedb"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/airoles"
)

// keys reports which keys exist; it records every key asked for and never has a value to give.
type keys struct {
	set   map[string]bool
	asked []string
}

func (k *keys) Get(_ context.Context, key string) (string, bool, error) {
	k.asked = append(k.asked, key)
	return "", k.set[key], nil
}

func newService(t *testing.T, k airoles.KeyStore) (*airoles.Service, *sql.DB) {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "pitha.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return airoles.New(system.NewRuntimeSettingsRepository(db), system.NewAIRoleRepository(db), k), db
}

func TestStateDefaultsThenSavedThenRunning(t *testing.T) {
	k := &keys{set: map[string]bool{config.KeyOpenRouterAPIKey: true}}
	svc, db := newService(t, k)
	ctx := context.Background()

	st, err := svc.State(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Roles) != len(airole.Roles()) || len(st.History) != 0 {
		t.Fatalf("state = %+v", st)
	}
	for _, r := range st.Roles {
		if r.Setting != airole.DefaultSetting() || r.Stored || r.Running != nil {
			t.Errorf("%s default = %+v", r.ID, r)
		}
	}
	if !st.Roles[0].Live || st.Roles[1].Live {
		t.Error("only Jev is the live role")
	}
	// The providers' key state is a flag per provider; the secrets store was asked only about those two keys.
	byID := map[string]airoles.Provider{}
	for _, p := range st.Providers {
		byID[p.ID] = p
	}
	if p := byID["openrouter"]; !p.KeyKnown || !p.KeySet {
		t.Errorf("openrouter = %+v", p)
	}
	if p := byID["custom"]; !p.KeyKnown || p.KeySet {
		t.Errorf("custom = %+v", p)
	}
	if len(k.asked) != 2 {
		t.Errorf("asked %v", k.asked)
	}

	if err := svc.Save(ctx, "sol", "openrouter", " vendor/model "); err != nil {
		t.Fatal(err)
	}
	if _, err := system.NewAIRoleRepository(db).RecordStarted(ctx, "sol", airole.DefaultSetting()); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.State(ctx)
	sol := st.Roles[1]
	if sol.Setting != (airole.Setting{Provider: "openrouter", Model: "vendor/model"}) || !sol.Stored || sol.Running == nil || *sol.Running != airole.DefaultSetting() {
		t.Errorf("sol = %+v", sol)
	}
	if len(st.History) != 2 || st.History[0].Saved || !st.History[1].Saved {
		t.Errorf("history = %+v", st.History)
	}

	if err := svc.Reset(ctx, "sol"); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.State(ctx)
	if st.Roles[1].Stored || st.Roles[1].Setting != airole.DefaultSetting() {
		t.Errorf("sol after Reset = %+v", st.Roles[1])
	}
}

func TestInvalidInputIsRejectedWithAReasonAndSavesNothing(t *testing.T) {
	svc, db := newService(t, nil)
	for _, tc := range [][3]string{{"jev", "openrouter", ""}, {"jev", "other", "m"}, {"nobody", "custom", ""}, {"sol", "custom", "m"}} {
		err := svc.Save(context.Background(), tc[0], tc[1], tc[2])
		if !errors.Is(err, airoles.ErrInvalidInput) || strings.Contains(err.Error(), "airole:") {
			t.Errorf("Save(%v) = %v, want an ErrInvalidInput with an operator-facing reason", tc, err)
		}
	}
	if err := svc.Reset(context.Background(), "nobody"); !errors.Is(err, airoles.ErrInvalidInput) {
		t.Errorf("Reset of an unknown role = %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM runtime_settings WHERE key LIKE 'ai.role.%'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("rows = %d (%v)", n, err)
	}
	// Without a key store the key state is unknown, not "unset".
	st, err := svc.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range st.Providers {
		if p.KeyKnown {
			t.Errorf("%s key state is known without a store", p.ID)
		}
	}
}
