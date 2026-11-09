package manifest_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest/manifesttest"
)

func frozen(t *testing.T) manifest.Manifest {
	t.Helper()
	m := manifesttest.Valid()
	if err := m.Freeze(m.CreatedAt.Add(time.Minute)); err != nil {
		t.Fatalf("Freeze: %v", err)
	}
	return m
}

func TestMinimumsAreUnsetByDefault(t *testing.T) {
	m := manifesttest.Valid()
	want := []string{"min_days", "min_count", "delta_label_bps", "delta_replay_daily_return", "power"}
	got := m.Minimums.UnsetFields()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unset = %v, want %v", got, want)
	}
	b, err := manifest.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range want {
		if !strings.Contains(string(b), `"`+f+`": null`) {
			t.Errorf("%s is not serialised as null", f)
		}
	}
}

func TestFreezeAndVerify(t *testing.T) {
	m := frozen(t)
	if err := m.Verify(); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if err := m.Freeze(m.Frozen.At); err == nil {
		t.Error("a frozen manifest was frozen again")
	}
	if err := manifesttest.Valid().Verify(); !errors.Is(err, manifest.ErrNotFrozen) {
		t.Errorf("unfrozen Verify = %v, want ErrNotFrozen", err)
	}
}

func TestChangeAfterFreezeIsDetected(t *testing.T) {
	for name, change := range map[string]func(*manifest.Manifest){
		"model swapped":     func(m *manifest.Manifest) { m.Model.Version = "lr-other" },
		"params hash":       func(m *manifest.Manifest) { m.Model.ParamsSHA256 = manifest.ParamsHash("other") },
		"threshold":         func(m *manifest.Manifest) { m.Rules.MinEVBps = 1 },
		"window":            func(m *manifest.Manifest) { m.Windows.EvalTo = m.Windows.EvalTo.Add(24 * time.Hour) },
		"seed":              func(m *manifest.Manifest) { m.Statistics.Seed++ },
		"minimum filled in": func(m *manifest.Manifest) { d := 5; m.Minimums.MinDays = &d },
		"run count":         func(m *manifest.Manifest) { m.Run.Count = 1; m.Run.Previous = nil; m.Provenance.Notes = nil },
	} {
		t.Run(name, func(t *testing.T) {
			m := frozen(t)
			change(&m)
			if err := m.Verify(); !errors.Is(err, manifest.ErrTampered) {
				t.Fatalf("Verify after change = %v, want ErrTampered", err)
			}
		})
	}
}

func TestCheckModelRefusesAnotherModel(t *testing.T) {
	m := frozen(t)
	if err := m.CheckModel(m.Model.Version, m.Model.ParamsSHA256); err != nil {
		t.Fatalf("frozen model refused: %v", err)
	}
	if err := m.CheckModel("lr-newer", m.Model.ParamsSHA256); !errors.Is(err, manifest.ErrModelMismatch) {
		t.Errorf("another version = %v, want ErrModelMismatch", err)
	}
	if err := m.CheckModel(m.Model.Version, manifest.ParamsHash("retrained")); !errors.Is(err, manifest.ErrModelMismatch) {
		t.Errorf("another params hash = %v, want ErrModelMismatch", err)
	}
}

func TestNewRunKeepsTheEarlierRunListed(t *testing.T) {
	first := frozen(t)
	d := 7
	second, err := manifest.NewRun(first, "min_days decided", first.Frozen.At.Add(time.Hour), func(m *manifest.Manifest) { m.Minimums.MinDays = &d })
	if err != nil {
		t.Fatalf("NewRun: %v", err)
	}
	if second.Run.Count != 2 || len(second.Run.Previous) != 1 || second.Run.Previous[0].SHA256 != first.Frozen.SHA256 || second.Run.Previous[0].Count != 1 {
		t.Fatalf("run = %+v", second.Run)
	}
	if second.Frozen != nil || first.Minimums.MinDays != nil {
		t.Error("NewRun must not freeze the new run or touch the earlier one")
	}
	if err := second.Freeze(second.CreatedAt); err != nil {
		t.Fatalf("Freeze second: %v", err)
	}
	third, err := manifest.NewRun(second, "again", second.Frozen.At, nil)
	if err != nil || third.Run.Count != 3 || len(third.Run.Previous) != 2 {
		t.Fatalf("third = %+v, %v", third.Run, err)
	}
	if _, err := manifest.NewRun(first, "", first.Frozen.At, nil); err == nil {
		t.Error("a new run without a reason was accepted")
	}
	if _, err := manifest.NewRun(manifesttest.Valid(), "x", time.Now(), nil); err == nil {
		t.Error("a new run from an unfrozen manifest was accepted")
	}
}

func TestRunCountMustMatchListedRuns(t *testing.T) {
	m := manifesttest.Valid()
	m.Run.Count = 3
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "run.count") {
		t.Fatalf("Validate = %v, want run.count error", err)
	}
}

func TestValidateRejectsIncompletePreRegistration(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(*manifest.Manifest)
		want   string
	}{
		"quality cut not good":   {func(m *manifest.Manifest) { m.Rules.QualityCut = "strong" }, "quality_cut"},
		"family not the primary": {func(m *manifest.Manifest) { m.Statistics.ComparisonFamily = m.Statistics.ComparisonFamily[:3] }, "comparison_family"},
		"too few comparisons":    {func(m *manifest.Manifest) { m.Statistics.NumComparisons = 3 }, "num_comparisons"},
		"unknown correction":     {func(m *manifest.Manifest) { m.Statistics.Correction = "holm" }, "correction"},
		"no db hash":             {func(m *manifest.Manifest) { m.DBCopy.SHA256 = "" }, "db_copy"},
		"bad model hash":         {func(m *manifest.Manifest) { m.Model.ParamsSHA256 = "x" }, "params_sha256"},
		"no eval window":         {func(m *manifest.Manifest) { m.Windows.EvalFrom = m.Windows.EvalTo }, "eval_from"},
		"negative embargo":       {func(m *manifest.Manifest) { m.Windows.EmbargoSeconds = -1 }, "embargo"},
		"provenance":             {func(m *manifest.Manifest) { m.Provenance.ChosenBeforeEvaluation = false }, "chosen_before_evaluation"},
		"min days of one":        {func(m *manifest.Manifest) { d := 1; m.Minimums.MinDays = &d }, "min_days"},
		"power out of range":     {func(m *manifest.Manifest) { p := 1.0; m.Minimums.Power = &p }, "power"},
		"no initial capital":     {func(m *manifest.Manifest) { m.Replay.InitialState.CapitalJPY = 0 }, "initial capital"},
		"exclusion without why": {func(m *manifest.Manifest) {
			m.Exclusions = []manifest.Exclusion{{Kind: "holiday", From: manifesttest.T0, To: manifesttest.T0.Add(time.Hour)}}
		}, "exclusions[0]"},
	} {
		t.Run(name, func(t *testing.T) {
			m := manifesttest.Valid()
			tc.change(&m)
			err := m.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestFileRoundTripAndNoOverwrite(t *testing.T) {
	m := frozen(t)
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := manifest.WriteNew(path, m); err != nil {
		t.Fatalf("WriteNew: %v", err)
	}
	if err := manifest.WriteNew(path, m); err == nil {
		t.Error("a frozen manifest file was overwritten")
	}
	got, err := manifest.ReadFrozen(path)
	if err != nil {
		t.Fatalf("ReadFrozen: %v", err)
	}
	if got.Frozen.SHA256 != m.Frozen.SHA256 {
		t.Error("hash changed over the file round trip")
	}
	if _, err := manifest.Unmarshal([]byte(`{"schema_version":1,"params_json":"{}"}`)); err == nil {
		t.Error("an unknown field (a params_json body) was accepted")
	}
}
