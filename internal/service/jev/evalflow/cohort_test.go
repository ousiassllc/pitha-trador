package evalflow_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/jevshadowsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// Issue #881: every registered Trader prompt version has a cohort. Adding a
// version without deciding whether its answers mean what an existing cohort's
// mean fails here, so no version can reach jev_decisions with an undefined
// treatment by the aggregation and the training.
func TestEveryRegisteredTraderVersionHasACohort(t *testing.T) {
	for _, v := range jev.TraderVersions() {
		if _, ok := jev.TraderCohort(v); !ok {
			t.Errorf("trader version %q has no cohort: add it to traderCohorts (cohort.go)", v)
		}
	}
	if _, ok := jev.TraderCohort(jev.TraderQuestionVersion); !ok {
		t.Errorf("the live build default %q has no cohort", jev.TraderQuestionVersion)
	}
}

// trader-v4 redefines entry_quality, which the meta-model reads as a feature, so
// it is another cohort than trader-v3; the retired v1/v2 rows stay with v3 (what
// today's database selects must not change).
func TestForeignVersions(t *testing.T) {
	for _, tc := range []struct {
		live string
		want []string
	}{
		{"trader-v3", []string{"trader-v4"}},
		{"trader-v4", []string{"trader-v1", "trader-v2", "trader-v3"}},
		{"trader-v2", []string{"trader-v4"}},
		{"trader-v9", []string{}}, // an unknown live version leaves nothing out
	} {
		if got := jev.ForeignVersions(tc.live); !slices.Equal(got, tc.want) {
			t.Errorf("ForeignVersions(%q) = %v, want %v", tc.live, got, tc.want)
		}
	}
}

type mapStore struct {
	values map[string]string
	err    error
}

func (s mapStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := s.values[key]
	return v, ok, s.err
}

// The scope follows the live version: the build default until a promotion
// stores another one; a failed read is an error, never "nothing to leave out".
func TestLiveCohortScope(t *testing.T) {
	ctx := context.Background()

	got, err := jev.LiveCohortScope(mapStore{})(ctx)
	if err != nil || !slices.Equal(got, []string{"trader-v4"}) {
		t.Fatalf("default live version: foreign = %v, %v, want [trader-v4]", got, err)
	}
	promoted := mapStore{values: map[string]string{jevshadowsettings.KeyLiveVersion: `"trader-v4"`}}
	got, err = jev.LiveCohortScope(promoted)(ctx)
	if err != nil || !slices.Equal(got, []string{"trader-v1", "trader-v2", "trader-v3"}) {
		t.Fatalf("promoted: foreign = %v, %v, want the trader-v3 cohort", got, err)
	}
	if _, err := jev.LiveCohortScope(mapStore{err: errors.New("db down")})(ctx); err == nil {
		t.Fatal("a failed read of the live version must be an error")
	}
}
