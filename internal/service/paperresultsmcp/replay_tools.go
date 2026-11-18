package paperresultsmcp

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/replaytool"
)

// The replay evaluation tools (issue #1085, docs/api/endpoints/paper-results-mcp-jev-replay.md).
// replay_start / replay_stop write only jev_replay_runs: no trading setting, not
// the replay switch (jev.replay.enabled is the Settings screen's), no request, no key.
func init() { tools = append(tools, replayTools...) }

var replayTools = []tool{
	replayTool("replay_estimate", replaytool.EstimateDescription, periodSchema(replaytool.PlanProperties()), false,
		func(ctx context.Context, e toolEnv, a struct {
			periodArgs
			replaytool.PlanArgs
		}) (any, int, error) {
			if err := a.Validate(); err != nil {
				return nil, 0, err
			}
			v, err := replaytool.Estimate(ctx, e.q, e.r, a.PlanArgs)
			return v, v.Calls, err
		}),
	replayTool("replay_start", replaytool.StartDescription, periodSchema(replaytool.StartProperties()), true,
		func(ctx context.Context, e toolEnv, a struct {
			periodArgs
			replaytool.StartArgs
		}) (any, int, error) {
			if err := a.Validate(); err != nil {
				return nil, 0, err
			}
			if a.ConfirmToken == "" {
				return nil, 0, failDetail(CodeInvalidArgument, "confirm_token は必須です（replay_estimate が返した値）")
			}
			w, err := e.writer()
			if err != nil {
				return nil, 0, err
			}
			run, err := w.StartReplay(ctx, e.client, e.r, a.StartArgs)
			return run, 1, err
		}),
	replayTool("replay_stop", replaytool.StopDescription, objectSchema(map[string]any{"run_id": replaytool.RunIDProperty()}, "run_id"), true,
		func(ctx context.Context, e toolEnv, a replaytool.RunIDArgs) (any, int, error) {
			if err := a.Validate(true); err != nil {
				return nil, 0, err
			}
			w, err := e.writer()
			if err != nil {
				return nil, 0, err
			}
			run, err := w.StopReplay(ctx, *a.RunID)
			return run, 1, err
		}),
	replayTool("replay_progress", replaytool.ProgressDescription, objectSchema(map[string]any{
		"run_id": replaytool.RunIDProperty(),
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": replaytool.MaxListLimit, "default": replaytool.DefaultListLimit, "description": "run_id を省略したときの件数"},
	}), false, func(ctx context.Context, e toolEnv, a struct {
		replaytool.RunIDArgs
		Limit *int `json:"limit"`
	}) (any, int, error) {
		if err := a.Validate(false); err != nil {
			return nil, 0, err
		}
		limit := replaytool.DefaultListLimit
		if a.Limit != nil {
			var terr *toolError
			if limit, terr = resolveLimit(a.Limit, replaytool.MaxListLimit); terr != nil {
				return nil, 0, terr
			}
		}
		list, err := replaytool.Progress(ctx, e.q, a.RunID, limit)
		return list, len(list.Runs), err
	}),
	replayTool("replay_results", replaytool.ResultsDescription, objectSchema(map[string]any{"run_id": replaytool.RunIDProperty(), "arms": replaytool.ArmsProperty()}, "run_id"), false,
		func(ctx context.Context, e toolEnv, a struct {
			replaytool.RunIDArgs
			Arms []string `json:"arms"`
		}) (any, int, error) {
			if err := a.Validate(true); err != nil {
				return nil, 0, err
			}
			v, err := replaytool.Results(ctx, e.q, *a.RunID, a.Arms)
			return v, v.Comparison.Cohort.Size, err
		}),
}

// replayTool builds a replay tool over strict arguments R (from/to inside R when it has a period).
func replayTool[R any](name, desc string, schema map[string]any, writes bool, run func(context.Context, toolEnv, R) (any, int, error)) tool {
	_, hasPeriod := schema["properties"].(map[string]any)["from"]
	return tool{name: name, description: desc, schema: schema, writes: writes, noPeriod: !hasPeriod,
		decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
			var a R
			var p periodArgs
			if err := strictDecode(raw, &a); err != nil {
				return p, nil, err
			}
			_ = json.Unmarshal(raw, &p) // from/to; R has the same member names when the tool takes a period
			return p, func(ctx context.Context, e toolEnv) (any, int, error) {
				v, n, err := run(ctx, e, a)
				return v, n, replayFailureOf(err)
			}, nil
		}}
}

// replayFailureOf maps a replay tool error to a spec error; others are left to classify.
func replayFailureOf(err error) error {
	if errors.Is(err, mcpset.ErrWriteDisabled) {
		return fail(CodeWriteDisabled)
	}
	invalid, detail, ok := replaytool.Failure(err)
	switch {
	case !ok:
		return err
	case invalid:
		return failDetail(CodeInvalidArgument, detail)
	}
	return failDetail(CodeInvalidValue, detail)
}
