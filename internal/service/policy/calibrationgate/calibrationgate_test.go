package calibrationgate_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config/metalabelsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/policy/calibrationgate"
)

// count is a Source answering ok (or err) and counting its reads.
type count struct {
	ok    bool
	err   error
	reads int
	min   int
}

func (c *count) BucketHasSamples(_ context.Context, _ float64, min int) (bool, error) {
	c.reads++
	c.min = min
	return c.ok, c.err
}

// live is the live version resolver of a settings value ("" is the build default).
func live(version string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		if version == "" {
			return "trader-v3", nil
		}
		return version, nil
	}
}

var promoted = live("trader-v4")

// Issue #881: the bucket's sample sufficiency is the live version's own. While it
// is not met, Paper alone may decide on the all-versions total (and the verdict
// says so); Live, and any mode that is not Paper, never reads the total.
func TestGate_LiveVersionOwnSamplesAndPaperOnlyFallback(t *testing.T) {
	for name, tc := range map[string]struct {
		mode         metalabelsettings.Mode
		own, all     bool
		wantOK       bool
		wantAllVers  bool
		wantAllReads int
	}{
		"Paper, own sufficient: passes on its own samples, total not read":     {mode: metalabelsettings.ModePaper, own: true, all: true, wantOK: true},
		"Paper, own insufficient, total sufficient: fallback, flagged":         {mode: metalabelsettings.ModePaper, all: true, wantOK: true, wantAllVers: true, wantAllReads: 1},
		"Paper, both insufficient: not calibrated":                             {mode: metalabelsettings.ModePaper, wantAllReads: 1},
		"Live, own sufficient: passes":                                         {mode: metalabelsettings.ModeLive, own: true, all: true, wantOK: true},
		"Live, own insufficient although the total is sufficient: no fallback": {mode: metalabelsettings.ModeLive, all: true},
		"mode unset behaves as Live: no fallback":                              {mode: "", all: true},
	} {
		t.Run(name, func(t *testing.T) {
			own, all := &count{ok: tc.own}, &count{ok: tc.all}
			got, err := calibrationgate.Gate{Source: own, AllVersions: all, Mode: tc.mode, Live: promoted}.Check(context.Background(), 0.68, 20)
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if got.OK != tc.wantOK || got.UsedAllVersions() != tc.wantAllVers || all.reads != tc.wantAllReads {
				t.Fatalf("Check = %+v, all-versions reads %d; want ok=%v allVersions=%v reads=%d", got, all.reads, tc.wantOK, tc.wantAllVers, tc.wantAllReads)
			}
			if tc.own && got.Detail != "" {
				t.Errorf("Detail = %q for a bucket that met the live version's own count, want none", got.Detail)
			}
			if !tc.own && !strings.Contains(got.Detail, "live version trader-v4 has fewer than 20 labeled samples") {
				t.Errorf("Detail = %q, want it to name trader-v4 and the minimum", got.Detail)
			}
			if own.min != 20 {
				t.Errorf("own count asked for %d samples, want 20", own.min)
			}
		})
	}
}

// The reason names the live version: the build default without a promotion.
func TestGate_DetailNamesTheLiveVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		live func(context.Context) (string, error)
		want string
	}{
		"build default": {live(""), "live version trader-v3 has fewer"},
		"promoted":      {promoted, "live version trader-v4 has fewer"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := calibrationgate.Gate{Source: &count{}, Mode: metalabelsettings.ModeLive, Live: tc.live}.Check(context.Background(), 0.68, 20)
			if err != nil || got.OK || !strings.HasPrefix(got.Detail, tc.want) {
				t.Fatalf("Check = %+v, %v, want not OK with %q", got, err, tc.want)
			}
		})
	}
}

// Without an all-versions source there is no fallback, even in Paper; the unnamed
// text applies without a store; a failed count is an error, not a pass.
func TestGate_NoFallbackWithoutAnAllVersionsSourceAndErrorsAreErrors(t *testing.T) {
	got, err := calibrationgate.Gate{Source: &count{}, Mode: metalabelsettings.ModePaper}.Check(context.Background(), 0.68, 20)
	if err != nil || got.OK || !strings.HasPrefix(got.Detail, "live version has fewer than 20") {
		t.Fatalf("Paper without an all-versions source: %+v, %v, want not OK with the unnamed text", got, err)
	}
	if _, err := (calibrationgate.Gate{Source: &count{err: errors.New("db down")}, Mode: metalabelsettings.ModePaper}).Check(context.Background(), 0.68, 20); err == nil {
		t.Fatal("a failed count must be an error, never a pass")
	}
}
