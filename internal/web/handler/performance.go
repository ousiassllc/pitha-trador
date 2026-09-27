package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/backtest"
	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// BacktestRunner runs a Walk Forward backtest (functional.md FR-BT-2) over
// the recorded history in [wf.Start, wf.End). internal/bootstrap's
// BacktestSource implements it.
type BacktestRunner interface {
	RunWalkForward(ctx context.Context, wf backtest.WalkForwardConfig) (backtest.Result, error)
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

const dateLayout = "2006-01-02"

// PerformanceHandler implements `GET /performance` (docs/api/endpoints.md
// §3), the Performance page and its backtest execution path (UC-12).
type PerformanceHandler struct {
	runner BacktestRunner
	now    func() time.Time
}

// NewPerformanceHandler returns a PerformanceHandler backed by runner.
func NewPerformanceHandler(runner BacktestRunner) *PerformanceHandler {
	return &PerformanceHandler{runner: runner, now: time.Now}
}

// Page implements `GET /performance`: without a `from` query parameter it
// renders the backtest form prefilled with defaults; with one it parses
// from/to/training_days/validation_days/forward_days, runs the Walk
// Forward backtest, and renders its metrics (400 on invalid input, 500 on
// a run failure, both with the error shown on the page).
func (h *PerformanceHandler) Page(c *gin.Context) {
	props := pages.PerformanceProps{Form: h.defaultForm()}
	status := http.StatusOK

	if c.Query("from") != "" {
		form, wf, err := parseBacktestForm(c)
		props.Form = form
		switch {
		case err != nil:
			status, props.Error = http.StatusBadRequest, err.Error()
		default:
			result, err := h.runner.RunWalkForward(c.Request.Context(), wf)
			if err != nil {
				status, props.Error = http.StatusInternalServerError, "backtest failed: "+err.Error()
			} else {
				props.Result = &result
			}
		}
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	_ = pages.PerformancePage(props).Render(c.Request.Context(), c.Writer)
}

func (h *PerformanceHandler) defaultForm() pages.PerformanceForm {
	today := h.now().In(jst)
	return pages.PerformanceForm{
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
func parseBacktestForm(c *gin.Context) (pages.PerformanceForm, backtest.WalkForwardConfig, error) {
	form := pages.PerformanceForm{From: c.Query("from"), To: c.Query("to")}
	var errs []error
	days := func(key string, fallback int) int {
		raw := c.Query(key)
		if raw == "" {
			return fallback
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			errs = append(errs, fmt.Errorf("%s must be a positive integer, got %q", key, raw))
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
	if len(errs) == 0 && to.Before(from) {
		errs = append(errs, fmt.Errorf("to (%s) must not be before from (%s)", form.To, form.From))
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
	if len(wf.Splits()) == 0 {
		return form, backtest.WalkForwardConfig{}, fmt.Errorf("%s..%s is too short for one %d+%d+%d-day Training/Validation/Forward fold",
			form.From, form.To, form.TrainingDays, form.ValidationDays, form.ForwardDays)
	}
	return form, wf, nil
}
