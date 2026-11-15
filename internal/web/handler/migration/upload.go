package migration

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// MaxUploadBytes caps one uploaded bundle (4 GiB: far above a real database,
// low enough to stop a runaway upload from filling the disk).
const MaxUploadBytes int64 = 4 << 30

// PreviewPath is the upload route. It is a plain gin route rather than a huma
// operation because huma buffers a request body in memory; the bundle is
// streamed to a staging file instead (the body is the raw file, not
// multipart).
const PreviewPath = "/api/v1/migration/preview"

// Register adds every migration endpoint: the huma operations on api and
// `POST /api/v1/migration/preview` on engine (the raw bundle file as the
// request body; the response is the importflow.Preview - what the bundle holds
// and what it would replace - as JSON; nothing in the live data changes).
func (h *Handler) Register(api huma.API, engine *gin.Engine) {
	huma.Get(api, "/migration/export", h.APIExport)
	huma.Get(api, "/migration/status", h.APIStatus)
	huma.Post(api, "/migration/confirm", h.APIConfirm)
	huma.Post(api, "/migration/cancel", h.APICancel)
	engine.POST(PreviewPath, h.preview)
}

func (h *Handler) preview(c *gin.Context) {
	ctx := c.Request.Context()
	if !middleware.Authenticated(ctx) {
		problem(c, http.StatusForbidden, "missing or invalid session cookie")
		return
	}
	body := http.MaxBytesReader(c.Writer, c.Request.Body, MaxUploadBytes)
	p, err := h.importer.Preview(ctx, body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			problem(c, http.StatusRequestEntityTooLarge, "ファイルが大きすぎます。")
			return
		}
		status, detail := classify(err)
		if status == http.StatusInternalServerError {
			slog.ErrorContext(ctx, "migration: preview", "error", err)
		}
		problem(c, status, detail)
		return
	}
	c.JSON(http.StatusOK, p)
}

// problem writes an RFC 7807 body like huma's, so the shared client
// (static/src/components/lib/api.ts) shows `detail`.
func problem(c *gin.Context, status int, detail string) {
	c.Header("Content-Type", "application/problem+json")
	c.JSON(status, gin.H{"title": http.StatusText(status), "status": status, "detail": detail})
}

// classify turns an import error into a status and an operator-facing
// (Japanese) message. Anything unknown is a 500 with a fixed message; the cause
// goes to the log.
func classify(err error) (int, string) {
	switch {
	case errors.Is(err, migratebundle.ErrSchemaTooNew):
		return http.StatusUnprocessableEntity, "このファイルは、より新しいバージョンのアプリで書き出されています。アプリを更新してから取り込んでください。"
	case errors.Is(err, migratebundle.ErrUnsupportedFormat):
		return http.StatusUnprocessableEntity, "このファイルの形式のバージョンは、このアプリでは読み込めません。"
	case errors.Is(err, migratebundle.ErrNotBundle):
		return http.StatusUnprocessableEntity, "PC 移行ファイルではありません。「PC 移行」で書き出したファイルを選んでください。"
	case errors.Is(err, migratebundle.ErrChecksum), errors.Is(err, migratebundle.ErrCorrupt):
		return http.StatusUnprocessableEntity, "ファイルが壊れているか、書き換えられています。もう一度書き出したファイルを使ってください。何も変更していません。"
	case errors.Is(err, migratebundle.ErrNotSecretFree):
		return http.StatusUnprocessableEntity, "このファイルには秘密情報（パスワード・APIキーなど）または移行対象外の設定が含まれているため取り込みません。"
	case errors.Is(err, importflow.ErrUnknownToken):
		return http.StatusConflict, "取り込みの確認の有効期限が切れました。もう一度ファイルを選んでください。"
	case errors.Is(err, importflow.ErrReplaceNotConfirmed):
		return http.StatusConflict, "現在のデータを置き換えることの確認が必要です。"
	}
	return http.StatusInternalServerError, "取り込みに失敗しました。"
}

func mapError(err error) error {
	status, detail := classify(err)
	if status == http.StatusInternalServerError {
		return huma.Error500InternalServerError(detail, err)
	}
	return huma.NewError(status, detail)
}
