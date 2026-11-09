package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/loader"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner/runnertest"
)

// asOf is after the evaluation days (Day(15) is the last).
var asOf = runnertest.Day(20)

// cli runs the command in-process and returns its output and error.
func cli(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err := run(context.Background(), args, &out, &errOut)
	return out.String() + errOut.String(), err
}

func mustCLI(t *testing.T, args ...string) string {
	t.Helper()
	out, err := cli(t, args...)
	if err != nil {
		t.Fatalf("metaablation %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// initArgs are the init flags of a database copy; outDir is outside the copy's directory.
func initArgs(db, outDir string, extra ...string) []string {
	return append([]string{
		"init", "-db", db, "-out", outDir, "-asof", asOf.Format(time.RFC3339),
		"-from", runnertest.Day(9).Format(time.RFC3339), "-to", runnertest.Day(20).Format(time.RFC3339),
	}, extra...)
}

func runArgs(db, mf, outDir string, extra ...string) []string {
	return append([]string{"run", "-db", db, "-manifest", mf, "-out", outDir}, extra...)
}

// prereg is the manifest path init writes for run 1 in dir.
func prereg(dir string) string { return filepath.Join(dir, "metaablation-run1.prereg.manifest.json") }

func sha(t *testing.T, path string) string {
	t.Helper()
	h, err := loader.FileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// report is the JSON document's parts the tests read.
type report struct {
	RunCount    int `json:"run_count"`
	Populations []struct {
		ID string `json:"id"`
	} `json:"populations"`
	Multiple struct {
		Executed       int  `json:"executed_comparisons"`
		FamilyExceeded bool `json:"family_exceeded"`
	} `json:"multiple_comparisons"`
	Label struct {
		Available bool   `json:"available"`
		Reason    string `json:"reason"`
		Result    struct {
			Cells []struct {
				Name               string `json:"name"`
				EligibleCandidates int    `json:"eligible_candidates"`
				Takes              int    `json:"takes"`
				Funnel             struct {
					Unknown int `json:"unknown"`
				} `json:"funnel"`
			} `json:"cells"`
			Comparisons []verdictRow `json:"comparisons"`
			Reference   struct {
				Rows int `json:"rows"`
			} `json:"reference_original_oos"`
			Independence struct {
				Independent int `json:"independent"`
				Reference   int `json:"reference"`
			} `json:"independence"`
		} `json:"result"`
	} `json:"label_layer"`
	Replay struct {
		Available          bool   `json:"available"`
		Reason             string `json:"reason"`
		UnavailablePeriods []struct {
			From, To, Reason string
		} `json:"unavailable_periods"`
		Segments []struct {
			Cells []struct {
				Name    string `json:"name"`
				Entries int    `json:"entries"`
				Stages  []struct {
					Stage string `json:"stage"`
				} `json:"stages"`
			} `json:"cells"`
			Differences []verdictRow `json:"daily_differences"`
		} `json:"segments"`
	} `json:"replay_layer"`
	Manifest manifest.Manifest `json:"manifest"`
}

type verdictRow struct {
	Name      string `json:"name"`
	Reference bool   `json:"reference"`
	Verdict   string `json:"verdict"`
}

func loadReport(t *testing.T, path string) report {
	t.Helper()
	var r report
	if err := json.Unmarshal(readFile(t, path), &r); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return r
}
