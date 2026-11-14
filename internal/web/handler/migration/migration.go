// Package migration implements the PC migration bundle endpoints (issue
// #1075, FR-MIGRATE-*): `GET /api/v1/migration/export` (one restorable file
// with the data and the non-secret settings), `POST /api/v1/migration/preview`
// (upload + validate, nothing changes), `POST /api/v1/migration/confirm` and
// `POST /api/v1/migration/cancel` (stage the import for the next start) and
// `GET /api/v1/migration/status` (what is pending / what the last start did).
// Like the other endpoints they need the session cookie (and, for POST, the
// CSRF token): the file is the operator's whole database. The application
// itself never replaces the live database: see internal/service/migratebundle/
// importflow.
package migration

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
	"github.com/ousiassllc/pitha-trador/internal/web/middleware"
)

// Exporter is the subset of *migratebundle.Exporter the download needs.
type Exporter interface {
	Export(ctx context.Context, w io.Writer) (migratebundle.Manifest, error)
}

// Importer is the subset of *importflow.Service the handler needs.
type Importer interface {
	Preview(ctx context.Context, upload io.Reader) (importflow.Preview, error)
	Confirm(ctx context.Context, token string, replaceExisting bool) (importflow.Pending, error)
	Cancel() error
	Pending() (*importflow.Pending, error)
	LastResult() (*importflow.Result, error)
}

// ErrNotConfigured is what Unconfigured returns.
var ErrNotConfigured = errors.New("migration: not configured (missing router.WithMigration)")

// Unconfigured fails everything: an entry point that forgets
// router.WithMigration gets a 500 (cause in the slog), not an empty bundle.
type Unconfigured struct{}

func (Unconfigured) Export(context.Context, io.Writer) (migratebundle.Manifest, error) {
	return migratebundle.Manifest{}, ErrNotConfigured
}

func (Unconfigured) Preview(context.Context, io.Reader) (importflow.Preview, error) {
	return importflow.Preview{}, ErrNotConfigured
}

func (Unconfigured) Confirm(context.Context, string, bool) (importflow.Pending, error) {
	return importflow.Pending{}, ErrNotConfigured
}
func (Unconfigured) Cancel() error                           { return ErrNotConfigured }
func (Unconfigured) Pending() (*importflow.Pending, error)   { return nil, ErrNotConfigured }
func (Unconfigured) LastResult() (*importflow.Result, error) { return nil, ErrNotConfigured }

// Handler serves the migration endpoints.
type Handler struct {
	exporter Exporter
	importer Importer
	now      func() time.Time
}

// New returns a Handler. A nil exporter / importer (an entry point that forgot
// router.WithMigration) is Unconfigured.
func New(exporter Exporter, importer Importer) *Handler {
	if exporter == nil {
		exporter = Unconfigured{}
	}
	if importer == nil {
		importer = Unconfigured{}
	}
	return &Handler{exporter: exporter, importer: importer, now: time.Now}
}

func requireSession(ctx context.Context) error {
	if !middleware.Authenticated(ctx) {
		return huma.Error403Forbidden("missing or invalid session cookie")
	}
	return nil
}

// ExportInput is empty: the bundle is always the whole database.
type ExportInput struct{}

// APIExport implements `GET /api/v1/migration/export`. The bundle is built in
// a temporary file (a failure never leaves a half-written download, and the
// database is not held in memory) and streamed back as an attachment. Nothing
// is sent anywhere else.
func (h *Handler) APIExport(ctx context.Context, _ *ExportInput) (*huma.StreamResponse, error) {
	if err := requireSession(ctx); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp("", "pitha-migration-*"+migratebundle.Extension)
	if err != nil {
		return nil, huma.Error500InternalServerError("export failed", err)
	}
	discard := func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	if _, err := h.exporter.Export(ctx, f); err != nil {
		discard()
		return nil, huma.Error500InternalServerError("export failed", err)
	}
	size, err := f.Seek(0, io.SeekCurrent)
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		discard()
		return nil, huma.Error500InternalServerError("export failed", err)
	}
	filename := "pitha-migration-" + h.now().UTC().Format("20060102-150405") + migratebundle.Extension
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer discard()
		hctx.SetHeader("Content-Type", "application/octet-stream")
		hctx.SetHeader("Content-Disposition", `attachment; filename="`+filename+`"`)
		hctx.SetHeader("Content-Length", strconv.FormatInt(size, 10))
		if _, err := io.Copy(hctx.BodyWriter(), f); err != nil {
			slog.WarnContext(ctx, "migration: send bundle", "error", err)
		}
	}}, nil
}

// ConfirmInput is `POST /api/v1/migration/confirm`'s body.
type ConfirmInput struct {
	Body struct {
		Token string `json:"token" required:"true" minLength:"32" maxLength:"32" doc:"The token of the preview."`
		// ReplaceExisting is the operator's explicit "replace my current data"; without it a database that has data is refused (409).
		ReplaceExisting bool `json:"replace_existing" doc:"True when the operator confirmed replacing the current data."`
	}
}

// ConfirmOutput is the import now waiting for the next start.
type ConfirmOutput struct{ Body importflow.Pending }

// APIConfirm implements `POST /api/v1/migration/confirm`: it stages the
// previewed bundle to be applied at the next start. The live database is not
// touched.
func (h *Handler) APIConfirm(ctx context.Context, in *ConfirmInput) (*ConfirmOutput, error) {
	if err := requireSession(ctx); err != nil {
		return nil, err
	}
	p, err := h.importer.Confirm(ctx, in.Body.Token, in.Body.ReplaceExisting)
	if err != nil {
		return nil, mapError(err)
	}
	return &ConfirmOutput{Body: p}, nil
}

// CancelOutput is empty on success.
type CancelOutput struct{}

// APICancel implements `POST /api/v1/migration/cancel`: it discards a staged
// preview and a pending import.
func (h *Handler) APICancel(ctx context.Context, _ *struct{}) (*CancelOutput, error) {
	if err := requireSession(ctx); err != nil {
		return nil, err
	}
	if err := h.importer.Cancel(); err != nil {
		return nil, huma.Error500InternalServerError("cancel failed", err)
	}
	return &CancelOutput{}, nil
}

// Status is `GET /api/v1/migration/status`'s body.
type Status struct {
	// Pending is the confirmed import that will be applied at the next start (omitted: none).
	Pending *importflow.Pending `json:"pending,omitempty"`
	// LastResult is what the last start did with an import (omitted: none ran).
	LastResult *importflow.Result `json:"last_result,omitempty"`
}

// StatusOutput wraps Status.
type StatusOutput struct{ Body Status }

// APIStatus implements `GET /api/v1/migration/status`.
func (h *Handler) APIStatus(ctx context.Context, _ *struct{}) (*StatusOutput, error) {
	if err := requireSession(ctx); err != nil {
		return nil, err
	}
	var s Status
	var err error
	if s.Pending, err = h.importer.Pending(); err != nil {
		return nil, huma.Error500InternalServerError("status failed", err)
	}
	if s.LastResult, err = h.importer.LastResult(); err != nil {
		return nil, huma.Error500InternalServerError("status failed", err)
	}
	return &StatusOutput{Body: s}, nil
}
