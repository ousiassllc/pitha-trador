package jevreplaysettings_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevreplaysettings"
)

type fakeStore struct {
	value string
	ok    bool
	err   error
}

func (f fakeStore) Get(context.Context, string) (string, bool, error) { return f.value, f.ok, f.err }

// The replay is off while nothing is stored, the value is damaged or the read
// fails: a fault can never turn it on.
func TestEnabled_OffUnlessStoredAsOne(t *testing.T) {
	for name, tc := range map[string]struct {
		store fakeStore
		want  bool
		err   bool
	}{
		"absent":   {fakeStore{}, false, false},
		"one":      {fakeStore{value: "1", ok: true}, true, false},
		"zero":     {fakeStore{value: "0", ok: true}, false, false},
		"two":      {fakeStore{value: "2", ok: true}, false, false},
		"text":     {fakeStore{value: `"on"`, ok: true}, false, false},
		"null":     {fakeStore{value: "null", ok: true}, false, false},
		"garbage":  {fakeStore{value: "{", ok: true}, false, false},
		"read err": {fakeStore{err: errors.New("boom")}, false, true},
	} {
		got, err := jevreplaysettings.Enabled(context.Background(), tc.store)
		if got != tc.want || (err != nil) != tc.err {
			t.Errorf("%s: Enabled = %v, %v; want %v, error %v", name, got, err, tc.want, tc.err)
		}
	}
	if jevreplaysettings.DefaultEnabled {
		t.Error("the replay is on by default")
	}
}

func TestSwitchRoundTrips(t *testing.T) {
	for _, on := range []bool{true, false} {
		got, err := jevreplaysettings.ParseEnabled(jevreplaysettings.Switch(on))
		if err != nil || got != on {
			t.Errorf("ParseEnabled(Switch(%v)) = %v, %v", on, got, err)
		}
	}
	if !jevreplaysettings.IsKey(jevreplaysettings.KeyEnabled) || jevreplaysettings.IsKey("jev.trial.enabled") {
		t.Error("IsKey does not tell the replay's keys from the trial's")
	}
}
