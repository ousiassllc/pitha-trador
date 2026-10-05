package performance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/service/insight"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// BacktestRunner runs a Walk Forward backtest (functional.md FR-BT-2) over
// the recorded history in [wf.Start, wf.End). internal/bootstrap/backtestsource's
// Source implements it.
type BacktestRunner interface {
	RunWalkForward(ctx context.Context, wf backtest.WalkForwardConfig) (backtest.Result, error)
}

// PerformanceSource reads the realized (Paper) performance of every closed
// position as of now (UC-9, functional.md §5.3). internal/service/insight.Reader
// implements it, as does insightapi.Provider.
type PerformanceSource interface {
	Performance(ctx context.Context, now time.Time) (insight.Performance, error)
}

// StaticBacktestRunner is internal/router.New()'s default BacktestRunner
// (router-level tests only): every run returns an empty Result.
type StaticBacktestRunner struct{}

func (StaticBacktestRunner) RunWalkForward(context.Context, backtest.WalkForwardConfig) (backtest.Result, error) {
	return backtest.Result{}, nil
}

// jst is the Tokyo Stock Exchange's time zone, which the Performance
// form's dates are day boundaries in. Japan has no DST, so a fixed zone
// is exact and needs no tzdata (absent on some Windows installs).
var jst = time.FixedZone("JST", 9*60*60)

// Default fold lengths and range for the Performance form: roughly one
// trading week of Training/Calibration, then two days of Validation and
// one day of Forward, over the last 20 calendar days.
const (
	defaultTrainingDays   = 5
	defaultValidationDays = 2
	defaultForwardDays    = 1
	defaultRangeDays      = 20
)

// Limits (issue #128): exceeding one is a 400 (huge day counts overflow
// time.Duration; tiny folds yield millions of Splits); a slow run is a 503.
const (
	maxFoldDays     = 366
	maxRangeDays    = 5 * 366
	maxSplits       = 1000
	backtestTimeout = 60 * time.Second
)

const dateLayout = "2006-01-02"

// PerformanceHandler implements `GET /performance` (docs/api/endpoints.md
// §3), the Performance page and its backtest execution path (UC-12).
type PerformanceHandler struct {
	runner  BacktestRunner
	actuals PerformanceSource
	now     func() time.Time
}

// NewPerformanceHandler returns a PerformanceHandler running backtests with
// runner and reading the realized performance from actuals.
func NewPerformanceHandler(runner BacktestRunner, actuals PerformanceSource) *PerformanceHandler {
	return &PerformanceHandler{runner: runner, actuals: actuals, now: time.Now}
}

// Page implements `GET /performance`: it always shows the realized (Paper)
// performance section (500 with the error shown in it if it cannot be
// read); then, without a `from` query parameter it
// renders the backtest form prefilled with defaults; with one it parses
// from/to/training_days/validation_days/forward_days, runs the Walk
// Forward backtest, and renders its metrics (400 on invalid input, 500 on
// a run failure, both with the error shown on the page).
func (h *PerformanceHandler) Page(c *gin.Context) {
	props := pages.PerformanceProps{Form: h.defaultForm()}
	status := http.StatusOK

	actuals, err := h.actuals.Performance(c.Request.Context(), h.now())
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "handler: performance actuals", "error", err)
		status, props.ActualsError = http.StatusInternalServerError, "実績の取得に失敗しました。"
	}
	props.Actuals = performanceActuals(actuals)

	if c.Query("from") != "" {
		form, wf, err := parseBacktestForm(c)
		props.Form = form
		switch {
		case err != nil:
			status, props.Error = http.StatusBadRequest, err.Error()
		default:
			ctx, cancel := context.WithTimeout(c.Request.Context(), backtestTimeout)
			defer cancel()
			result, err := h.runner.RunWalkForward(ctx, wf)
			switch {
			case errors.Is(err, context.DeadlineExceeded):
				status, props.Error = http.StatusServiceUnavailable, fmt.Sprintf("backtest timed out after %s; narrow the range", backtestTimeout)
			case err != nil:
				slog.ErrorContext(ctx, "handler: performance backtest", "error", err)
				status, props.Error = http.StatusInternalServerError, "バックテストの実行に失敗しました。"
			default:
				props.Result = performanceResult(result)
			}
		}
	}

	shared.RenderHTML(c, status, pages.PerformancePage(props))
}

func (h *PerformanceHandler) defaultForm() organisms.BacktestFormValues {
	today := h.now().In(jst)
	return organisms.BacktestFormValues{
		From:           today.AddDate(0, 0, -defaultRangeDays).Format(dateLayout),
		To:             today.Format(dateLayout),
		TrainingDays:   defaultTrainingDays,
		ValidationDays: defaultValidationDays,
		ForwardDays:    defaultForwardDays,
	}
}

// parseBacktestForm reads the Performance form's query parameters into a
// WalkForwardConfig over [from 00:00 JST, to+1 00:00 JST), stepping one
// Forward period per fold. The returned form echoes the raw input even on
// error.
func parseBacktestForm(c *gin.Context) (organisms.BacktestFormValues, backtest.WalkForwardConfig, error) {
	form := organisms.BacktestFormValues{From: c.Query("from"), To: c.Query("to")}
	var errs []error
	days := func(key string, fallback int) int {
		raw := c.Query(key)
		if raw == "" {
			return fallback
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > maxFoldDays {
			errs = append(errs, fmt.Errorf("%s must be an integer in 1..%d, got %q", key, maxFoldDays, raw))
			return fallback
		}
		return n
	}
	form.TrainingDays = days("training_days", defaultTrainingDays)
	form.ValidationDays = days("validation_days", defaultValidationDays)
	form.ForwardDays = days("forward_days", defaultForwardDays)

	from, err := time.ParseInLocation(dateLayout, form.From, jst)
	if err != nil {
		errs = append(errs, fmt.Errorf("from must be a YYYY-MM-DD date, got %q", form.From))
	}
	to, err := time.ParseInLocation(dateLayout, form.To, jst)
	if err != nil {
		errs = append(errs, fmt.Errorf("to must be a YYYY-MM-DD date, got %q", form.To))
	}
	if len(errs) == 0 {
		// int64 seconds: extreme dates must not overflow time.Duration.
		if days := (to.Unix()-from.Unix())/(24*60*60) + 1; to.Before(from) {
			errs = append(errs, fmt.Errorf("to (%s) must not be before from (%s)", form.To, form.From))
		} else if days > maxRangeDays {
			errs = append(errs, fmt.Errorf("from..to spans %d days, at most %d are allowed", days, maxRangeDays))
		}
	}
	if len(errs) > 0 {
		return form, backtest.WalkForwardConfig{}, errors.Join(errs...)
	}
	wf := backtest.WalkForwardConfig{
		Start:            from,
		End:              to.AddDate(0, 0, 1),
		TrainingPeriod:   time.Duration(form.TrainingDays) * 24 * time.Hour,
		ValidationPeriod: time.Duration(form.ValidationDays) * 24 * time.Hour,
		ForwardPeriod:    time.Duration(form.ForwardDays) * 24 * time.Hour,
	}
	switch n := wf.SplitCount(); {
	case n == 0:
		return form, backtest.WalkForwardConfig{}, fmt.Errorf("%s..%s is too short for one %d+%d+%d-day Training/Validation/Forward fold",
			form.From, form.To, form.TrainingDays, form.ValidationDays, form.ForwardDays)
	case n > maxSplits:
		return form, backtest.WalkForwardConfig{}, fmt.Errorf("%d folds would run, at most %d are allowed; lengthen forward_days or shorten the range", n, maxSplits)
	}
	return form, wf, nil
}
