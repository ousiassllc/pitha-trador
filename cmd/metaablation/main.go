// Command metaablation is the offline 2x2 comparison of the meta-labeling
// model (M axis) and the entry_quality cut-off (Q axis) on a COPY of the
// database (issue #1046; docs/research/metaablation.md). Obtaining the copy
// needs a separate approval; this command never touches the original.
//
// It has two steps. `init` pins one rejected model (the latest one, or the
// version named by -model) and pre-registers every rule value in a frozen
// manifest before any evaluation-window data is read; `run` executes the frozen manifest and writes the JSON and Markdown
// aggregates and the manifest to a directory outside the copy's directory.
//
//	metaablation init -db copy.db -asof 2026-11-09T00:00:00+09:00 \
//	    -from 2026-10-26 -to 2026-11-09 -out /var/tmp/ablation
//	metaablation run -db copy.db -manifest /var/tmp/ablation/metaablation-run1.prereg.manifest.json \
//	    -out /var/tmp/ablation/out1
//
// Without -model init pins the newest rejected model, so a nightly retraining
// moves both the model and the earliest valid -from. -model <version> pins that
// stored rejected model instead (the manifest records "explicit" rather than
// "latest_rejected"); -from must then be at or after THAT model's original OOS
// end, and a model stored later is never picked up by run:
//
//	metaablation init ... -model lr-20261110-090152 -from 2026-11-10 ...
//
// A second init for the same copy and model must name the one it replaces:
//
//	metaablation init ... -out /var/tmp/ablation \
//	    -supersedes /var/tmp/ablation/metaablation-run1.prereg.manifest.json \
//	    -new-run-reason "min-days confirmed" -min-days 15
//
// The database is opened with SQLite mode=ro only (no migration; a write
// fails), its SHA-256 is recorded by init and verified before and after run.
// A copy whose -wal file is not empty is refused (checkpoint it first); mode=ro
// may still create empty -wal/-shm files beside the copy. No LLM is called,
// nothing is trained or calibrated, nothing is ordered.
//
// Exit codes: 0 success; 1 failure (I/O, engine); 2 usage; 3 refusal (manifest
// changed or not frozen, another copy or model, -model naming a version that is
// not stored, not rejected or without readable parameters, not-checkpointed
// copy, output inside the copy, a changed value without a new run); 4 no layer
// executable.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/service/marketcalendar"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/manifest"
	"github.com/ousiassllc/pitha-trador/internal/service/metaablation/runner"
)

// Exit codes: 0 success (and -h); 1 a failure (I/O, the engine); 2 a usage
// mistake (flags, a missing or malformed value); 3 a deliberate refusal (a
// manifest that was changed or is not frozen, a database copy or model that is
// not the one frozen, a not-checkpointed copy, an output inside the copy, a
// changed value without a new run); 4 the run completed but no layer was
// executable (the reasons are in the report).
const (
	exitFailure       = 1
	exitUsage         = 2
	exitRefused       = 3
	exitNotExecutable = 4
)

// errUsage marks a usage mistake; errNotExecutable the no-layer-ran outcome.
var (
	errUsage         = errors.New("usage")
	errNotExecutable = errors.New("no layer was executable (see the reasons above and in the report)")
)

func usageErr(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{errUsage}, args...)...)
}

// exitCode maps an error of run to the process exit code.
func exitCode(err error) int {
	switch {
	case err == nil, errors.Is(err, flag.ErrHelp):
		return 0
	case errors.Is(err, errNotExecutable):
		return exitNotExecutable
	case errors.Is(err, runner.ErrRefused), errors.Is(err, manifest.ErrNotFrozen), errors.Is(err, manifest.ErrTampered), errors.Is(err, manifest.ErrModelMismatch):
		return exitRefused
	case errors.Is(err, errUsage):
		return exitUsage
	}
	return exitFailure
}

func main() {
	err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)
	if err != nil && !errors.Is(err, flag.ErrHelp) {
		_, _ = fmt.Fprintln(os.Stderr, "metaablation:", err)
	}
	os.Exit(exitCode(err))
}

const usage = "usage: metaablation init|run [flags] (see docs/research/metaablation.md)"

// parse parses flags: -h is flag.ErrHelp (exit 0), any other flag error is a usage mistake.
func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return fmt.Errorf("%w: %w", errUsage, err)
	}
	return nil
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageErr("%s", usage)
	}
	switch args[0] {
	case "init":
		return runInit(ctx, args[1:], stdout, stderr)
	case "run":
		return runRun(ctx, args[1:], stdout, stderr)
	}
	return usageErr("unknown subcommand %q; %s", args[0], usage)
}

// bindFloat is a float flag that sets *dst and leaves it nil when not given.
func bindFloat(fs *flag.FlagSet, name, usage string, dst **float64) {
	fs.Func(name, usage, func(s string) error {
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		*dst = &v
		return nil
	})
}

func bindInt(fs *flag.FlagSet, name, usage string, dst **int) {
	fs.Func(name, usage, func(s string) error {
		v, err := strconv.Atoi(s)
		if err != nil {
			return err
		}
		*dst = &v
		return nil
	})
}

// minimumFlags are the pre-registered thresholds of the verdict. They stay
// unset (every verdict 優劣未確定) until the proposal in
// docs/research/metaablation-thresholds.md is confirmed.
type minimumFlags struct{ m manifest.Minimums }

func addMinimumFlags(fs *flag.FlagSet) *minimumFlags {
	f := &minimumFlags{}
	bindInt(fs, "min-days", "pre-registered minimum trading days (unset by default)", &f.m.MinDays)
	bindInt(fs, "min-count", "pre-registered minimum paired-difference count (unset by default)", &f.m.MinCount)
	bindFloat(fs, "delta-label-bps", "pre-registered meaningful difference of the label layer, bps per eligible candidate (unset by default)", &f.m.DeltaLabelBps)
	bindFloat(fs, "delta-replay-daily-return", "pre-registered meaningful difference of the replay layer, daily return on capital (unset by default)", &f.m.DeltaReplayDailyReturn)
	bindFloat(fs, "power", "pre-registered power of the detectable minimum difference (unset by default)", &f.m.Power)
	return f
}

func (f *minimumFlags) minimums() manifest.Minimums { return f.m }

// parseTime accepts RFC3339 or a JST date (YYYY-MM-DD, midnight JST).
func parseTime(name, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, usageErr("-%s is required (RFC3339 or YYYY-MM-DD, JST)", name)
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, marketcalendar.JST)
	if err != nil {
		return time.Time{}, usageErr("-%s %q: not RFC3339 or YYYY-MM-DD: %v", name, s, err)
	}
	return t, nil
}

func runInit(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("metaablation init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "path to the database COPY (opened read-only)")
	out := fs.String("out", "", "output directory, outside the copy's directory")
	asof := fs.String("asof", "", "the time the copy reflects (RFC3339 or YYYY-MM-DD JST)")
	from := fs.String("from", "", "evaluation window start (inclusive; RFC3339 or YYYY-MM-DD JST)")
	to := fs.String("to", "", "evaluation window end (exclusive; RFC3339 or YYYY-MM-DD JST)")
	model := fs.String("model", "", "version of the rejected model to compare (default: the latest rejected model); -from must be at or after its original OOS end")
	cohort := fs.String("cohort", "", "cohort / Jev version, only when the model row records none")
	seed := fs.Int64("seed", runner.DefaultSeed, "bootstrap seed, recorded in the manifest")
	embargo := fs.Duration("embargo", 0, "walk-forward embargo (default and minimum: the training default, 30m)")
	supersedes := fs.String("supersedes", "", "the earlier pre-registration manifest of the same copy and model this one replaces (required when -out already holds one)")
	reason := fs.String("new-run-reason", "", "why the study is repeated (required with -supersedes)")
	var ro runner.RuleOverrides
	for _, r := range []struct {
		name string
		dst  **float64
	}{
		{"min-ev-bps", &ro.MinEVBps}, {"initial-capital", &ro.InitialCapitalJPY}, {"max-spread-bps", &ro.MaxSpreadBps},
		{"min-turnover-5m-jpy", &ro.MinTurnover5mJPY}, {"min-continuation", &ro.MinContinuation},
		{"max-toxic-flow", &ro.MaxToxicFlow}, {"max-liquidity-stressed", &ro.MaxLiquidityStressed},
	} {
		bindFloat(fs, r.name, "pre-registered rule value (default: the shipped one); recorded in the manifest", r.dst)
	}
	mins := addMinimumFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	if *dbPath == "" {
		return usageErr("-db is required")
	}
	var o runner.InitOptions
	var err error
	if o.AsOf, err = parseTime("asof", *asof); err != nil {
		return err
	}
	if o.EvalFrom, err = parseTime("from", *from); err != nil {
		return err
	}
	if o.EvalTo, err = parseTime("to", *to); err != nil {
		return err
	}
	o.DBPath, o.OutDir, o.Cohort, o.Seed, o.Embargo, o.Rules, o.Minimums = *dbPath, *out, *cohort, *seed, *embargo, ro, mins.minimums()
	o.Supersedes, o.NewRunReason, o.ModelVersion = *supersedes, *reason, *model
	path, m, err := runner.Init(ctx, o)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "pre-registered run %d: model %s (%s), cohort %s, window [%s, %s)\n", m.Run.Count, m.Model.Version, m.Model.Selection, m.Cohort,
		m.Windows.EvalFrom.Format(time.RFC3339), m.Windows.EvalTo.Format(time.RFC3339))
	if u := m.Minimums.UnsetFields(); len(u) > 0 {
		_, _ = fmt.Fprintf(stdout, "unset pre-registered values (every verdict will be 優劣未確定): %v\n", u)
	}
	_, _ = fmt.Fprintf(stdout, "manifest: %s\n", path)
	return nil
}

func runRun(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("metaablation run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbPath := fs.String("db", "", "path to the database COPY (opened read-only)")
	mpath := fs.String("manifest", "", "the frozen manifest written by init")
	out := fs.String("out", "", "output directory, outside the copy's directory")
	reason := fs.String("new-run-reason", "", "make this a NEW run (run count + 1) with the minimums given by flags; the reason is recorded")
	mins := addMinimumFlags(fs)
	if err := parse(fs, args); err != nil {
		return err
	}
	if *dbPath == "" || *mpath == "" {
		return usageErr("-db and -manifest are required")
	}
	sum, err := runner.Run(ctx, runner.RunOptions{DBPath: *dbPath, ManifestPath: *mpath, OutDir: *out, NewRunReason: *reason, Minimums: mins.minimums()})
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "run %d: model %s\n", sum.Manifest.Run.Count, sum.Manifest.Model.Version)
	for _, l := range []struct {
		name string
		s    runner.LayerStatus
	}{{"label layer", sum.Label}, {"replay layer", sum.Replay}} {
		if l.s.Available {
			_, _ = fmt.Fprintf(stdout, "%s: done\n", l.name)
		} else {
			_, _ = fmt.Fprintf(stdout, "%s: %s\n", l.name, l.s.Reason)
		}
	}
	if sum.PreRegistration != "" {
		_, _ = fmt.Fprintf(stdout, "new pre-registration: %s\n", sum.PreRegistration)
	}
	_, _ = fmt.Fprintf(stdout, "report: %s\n        %s\n        %s\n", sum.Files.JSON, sum.Files.Markdown, sum.Files.Manifest)
	if !sum.Label.Available && !sum.Replay.Available {
		return errNotExecutable
	}
	return nil
}
