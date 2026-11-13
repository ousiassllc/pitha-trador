package jevtrial_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/jevtrialsettings"
)

type keyStore struct {
	set bool
	err error
}

func (k keyStore) Get(_ context.Context, key string) (string, bool, error) {
	if key != config.KeyOpenRouterAPIKey {
		return "", false, errors.New("asked for another key")
	}
	return "sk-test", k.set, k.err
}

func TestState_ReportsWhetherTheKeyIsSetNeverItsValue(t *testing.T) {
	e := newEnv(t)
	for name, tc := range map[string]struct {
		ks              keyStore
		wantKnown, want bool
	}{
		"set":     {keyStore{set: true}, true, true},
		"unset":   {keyStore{}, true, false},
		"unknown": {keyStore{err: errors.New("db")}, false, false},
	} {
		st, err := e.s.WithKeyStore(tc.ks).State(context.Background())
		if err != nil || st.KeyKnown != tc.wantKnown || st.KeySet != tc.want {
			t.Errorf("%s: known=%v set=%v err=%v", name, st.KeyKnown, st.KeySet, err)
		}
	}
}

func TestState_DamagedArmsAreIgnoredWithAnError(t *testing.T) {
	e := newEnv(t)
	e.set(t, jevtrialsettings.KeyArms, `{"broken":true}`)
	st, err := e.s.State(context.Background())
	if err != nil || len(st.Arms) != 0 || st.ArmsError == "" || st.Enabled {
		t.Errorf("state = %+v, %v", st, err)
	}
}
