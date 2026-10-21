package mcpsettings

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/shared"
	"github.com/ousiassllc/pitha-trador/internal/web/organisms/mcp"
)

// ExperimentGate is the Settings screen's side of the tuning experiments
// (issue #871); the application's *mcpset.Gate implements it. The Settings
// screen is a person at the screen, so creating and ending an experiment here
// does not look at the MCP write switch.
type ExperimentGate interface {
	Experiments(ctx context.Context, limit int) ([]experiment.Summary, error)
	Compare(ctx context.Context, id int64) (experiment.Comparison, error)
	CreateExperiment(ctx context.Context, req experiment.Request) (experiment.Experiment, error)
	CloseExperiment(ctx context.Context, id int64, status string) (experiment.Experiment, error)
}

// experimentRows is how many experiments the list shows (newest first); every
// row costs two aggregate scans.
const experimentRows = 20

// datetimeLocalLayout is what <input type="datetime-local"> submits.
const datetimeLocalLayout = "2006-01-02T15:04"

// Experiments implements `GET /settings/mcp/experiments`: the list with each
// experiment's verdict and the form that records one.
func (h *Handler) Experiments(c *gin.Context) {
	h.renderExperiments(c, "")
}

// CompareExperiment implements `GET /settings/mcp/experiments/:id`: one
// experiment's before / after comparison.
func (h *Handler) CompareExperiment(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	id, ok := experimentID(c)
	if !ok {
		return
	}
	cmp, err := h.gate.Compare(ctx, id)
	if errors.Is(err, experiment.ErrNotFound) {
		shared.RespondActionError(c, http.StatusNotFound, "その実験はありません。")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: compare experiment", "id", id, "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "実験の前後比較を取得できませんでした。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, mcp.ExperimentDetail(detailProps(cmp)))
}

// CreateExperiment implements `POST /settings/mcp/experiments`: hypothesis,
// kind, name, before, after and an optional started_at (JST, from a
// datetime-local input). The form has exactly one variable. An invalid request
// is a 422 with the reason and nothing is stored.
func (h *Handler) CreateExperiment(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	args := experiment.Args{
		Hypothesis: c.PostForm("hypothesis"),
		Changes: []experiment.Change{{
			Kind: c.PostForm("kind"), Name: c.PostForm("name"), Before: c.PostForm("before"), After: c.PostForm("after"),
		}},
	}
	if v := c.PostForm("started_at"); v != "" {
		t, err := time.ParseInLocation(datetimeLocalLayout, v, query.JST)
		if err != nil {
			shared.RespondActionError(c, http.StatusUnprocessableEntity, "開始時刻の形式が正しくありません。")
			return
		}
		args.StartedAt = t.Format(time.RFC3339)
	}
	req, err := args.Validate(time.Now())
	if err == nil {
		_, err = h.gate.CreateExperiment(ctx, req)
	}
	if reason, invalid := experiment.IsInvalid(err); invalid {
		shared.RespondActionError(c, http.StatusUnprocessableEntity, "実験を記録できません: "+reason+"。")
		return
	}
	if errors.Is(err, experiment.ErrKeyNotAllowed) {
		shared.RespondActionError(c, http.StatusUnprocessableEntity, "その設定キーは MCP から変更できるキーではないため、実験にできません（settings_get に出るキーだけ）。")
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: create experiment", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "実験を記録できませんでした。")
		return
	}
	h.renderExperiments(c, "実験を記録しました。")
}

// CloseExperiment implements `POST /settings/mcp/experiments/:id/close`: form
// `status` is closed (終了) or aborted (中止). The end time is now.
func (h *Handler) CloseExperiment(c *gin.Context) {
	noStore(c)
	ctx := c.Request.Context()
	id, ok := experimentID(c)
	if !ok {
		return
	}
	status := c.PostForm("status")
	_, err := h.gate.CloseExperiment(ctx, id, status)
	switch {
	case errors.Is(err, experiment.ErrNotFound):
		shared.RespondActionError(c, http.StatusNotFound, "その実験はありません。")
		return
	case errors.Is(err, experiment.ErrNotRunning):
		shared.RespondActionError(c, http.StatusConflict, "この実験はもう終わっています。")
		return
	case err != nil:
		if reason, invalid := experiment.IsInvalid(err); invalid {
			shared.RespondActionError(c, http.StatusUnprocessableEntity, "実験を終えられません: "+reason+"。")
			return
		}
		slog.ErrorContext(ctx, "mcpsettings: close experiment", "id", id, "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "実験を終えられませんでした。")
		return
	}
	notice := "実験を終了しました。"
	if status == experiment.StatusAborted {
		notice = "実験を中止しました（中止した実験は判定しません）。"
	}
	h.renderExperiments(c, notice)
}

func (h *Handler) renderExperiments(c *gin.Context, notice string) {
	noStore(c)
	ctx := c.Request.Context()
	rows, err := h.gate.Experiments(ctx, experimentRows)
	if err != nil {
		slog.ErrorContext(ctx, "mcpsettings: list experiments", "error", err)
		shared.RespondActionError(c, http.StatusInternalServerError, "実験の一覧を取得できませんでした。")
		return
	}
	shared.RenderHTML(c, http.StatusOK, mcp.Experiments(experimentsProps(rows, notice)))
}

func experimentID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		shared.RespondActionError(c, http.StatusNotFound, "その実験はありません。")
		return 0, false
	}
	return id, true
}
