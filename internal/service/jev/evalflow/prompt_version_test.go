package evalflow_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/jev"
)

// pinnedPromptHashes binds each registered prompt version to the SHA-256 of
// its wire question group (every Instructions text and option Rubric, in
// order). Changing any wording without a new version fails
// TestPromptHashesAreBoundToVersions (issue #872).
//
// If this test fails because you edited a wording: do NOT update the hash of
// an existing version. Add a new version (prompts.go), register it, and pin its
// hash here. jev_decisions rows recorded under a version must keep meaning what
// that version said.
var pinnedPromptHashes = map[string]string{
	"trader-v3": "7a14873f4c3829ad8c91d283286d1dffb97ca9e0718e4ce9b52cad4b4df9b0d4",
	"trader-v4": "a9c70c854bfa41b6768f83467ed2832073143e37de3783b8f2b10bab946a9708",
}

const pinnedScoutV3Hash = "8b9b53ba08d17f25040c7be33aef341674cb4fae750d16f9f861d75d2ec0cdde"

func TestPromptHashesAreBoundToVersions(t *testing.T) {
	got := jev.TraderVersions()
	want := make([]string, 0, len(pinnedPromptHashes))
	for v := range pinnedPromptHashes {
		want = append(want, v)
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("registered trader versions = %v, pinned = %v: pin a hash for every registered version", got, want)
	}
	for version, pinned := range pinnedPromptHashes {
		hash, err := jev.TraderPromptHash(version)
		if err != nil {
			t.Fatalf("TraderPromptHash(%q): %v", version, err)
		}
		if hash != pinned {
			t.Errorf("version %q: the question wording hashes to %s, pinned %s.\n"+
				"The wording changed without a new version. Add a new version instead of editing %q (see prompts.go).", version, hash, pinned, version)
		}
	}
}

func TestScoutPromptHashIsBoundToItsVersion(t *testing.T) {
	hash, err := jev.ScoutPromptHash()
	if err != nil {
		t.Fatal(err)
	}
	if hash != pinnedScoutV3Hash {
		t.Errorf("scout wording hashes to %s, pinned %s for %q: bump jev.ScoutQuestionVersion and pin the new hash", hash, pinnedScoutV3Hash, jev.ScoutQuestionVersion)
	}
	if jev.ScoutQuestionVersion != "scout-v3" {
		t.Errorf("ScoutQuestionVersion = %q: re-pin pinnedScoutV3Hash for the new version", jev.ScoutQuestionVersion)
	}
}

func TestEveryVersionIsDistinctAndLookupFailsForUnknown(t *testing.T) {
	seen := map[string]string{}
	for _, v := range jev.TraderVersions() {
		hash, err := jev.TraderPromptHash(v)
		if err != nil {
			t.Fatal(err)
		}
		if other, dup := seen[hash]; dup {
			t.Errorf("versions %q and %q have identical wording: a version is a distinct prompt", other, v)
		}
		seen[hash] = v
	}
	if _, err := jev.TraderQuestionsFor("trader-v999"); err == nil {
		t.Error("an unregistered version must be an error, never sent")
	}
	if q, err := jev.TraderQuestionsFor(""); err != nil || q == nil {
		t.Errorf("empty version = build default %q: %v", jev.TraderQuestionVersion, err)
	}
	if !jev.IsTraderVersion(jev.TraderQuestionVersion) || !jev.IsTraderVersion(jev.TraderEntryQualityCandidateVersion) {
		t.Error("the live default and the candidate must both be registered")
	}
}

// The live default stays trader-v3 (issue #872: the first candidate is not
// promoted by shipping it).
func TestBuildDefaultIsStillTraderV3AndCandidateDiffersOnlyInEntryQuality(t *testing.T) {
	if jev.TraderQuestionVersion != "trader-v3" {
		t.Fatalf("TraderQuestionVersion = %q, want trader-v3", jev.TraderQuestionVersion)
	}
	v3, _ := jev.TraderQuestionsFor("trader-v3")
	v4, _ := jev.TraderQuestionsFor(jev.TraderEntryQualityCandidateVersion)
	if len(v3) != len(v4) {
		t.Fatalf("question counts differ: %d vs %d", len(v3), len(v4))
	}
	for id, q := range v3 {
		same := strings.Compare(string(mustJSON(t, q)), string(mustJSON(t, v4[id]))) == 0
		if id == "entry_quality" && same {
			t.Error("the candidate must redefine entry_quality")
		}
		if id != "entry_quality" && !same {
			t.Errorf("question %q differs between trader-v3 and the candidate: only entry_quality may", id)
		}
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
