package apierror_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"

	"github.com/ousiassllc/pitha-trador/internal/web/apierror"
)

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type failOutput struct {
	Body struct {
		OK bool `json:"ok"`
	}
}

type queryInput struct {
	Limit int `query:"limit" minimum:"1"`
}

func newAPI(t *testing.T, handler func(context.Context, *queryInput) (*failOutput, error)) humatest.TestAPI {
	t.Helper()
	apierror.Install()
	_, api := humatest.New(t)
	huma.Get(api, "/x", handler)
	return api
}

func TestInstall_InternalErrorHidesCauseAndLogsIt(t *testing.T) {
	logs := captureSlog(t)
	cause := errors.New("sqlite: no such table: secret_table (/var/lib/pitha/db.sqlite)")
	api := newAPI(t, func(context.Context, *queryInput) (*failOutput, error) {
		return nil, huma.Error500InternalServerError("read failed", cause)
	})

	resp := api.Get("/x?limit=1")

	if resp.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.Code)
	}
	if strings.Contains(resp.Body.String(), "secret_table") || strings.Contains(resp.Body.String(), "sqlite") {
		t.Fatalf("response leaks cause: %s", resp.Body.String())
	}
	var body huma.ErrorModel
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if body.Detail != "read failed" || len(body.Errors) != 0 {
		t.Fatalf("body = %+v, want detail %q and no errors", body, "read failed")
	}
	if !strings.Contains(logs.String(), "secret_table") || !strings.Contains(logs.String(), "level=ERROR") {
		t.Fatalf("slog missing cause at ERROR level: %q", logs.String())
	}
}

func TestInstall_ClientErrorsKeepDetails(t *testing.T) {
	logs := captureSlog(t)
	api := newAPI(t, func(context.Context, *queryInput) (*failOutput, error) {
		return nil, huma.Error404NotFound("unknown symbol")
	})

	validation := api.Get("/x?limit=0")
	if validation.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (body=%s)", validation.Code, validation.Body.String())
	}
	var body huma.ErrorModel
	if err := json.Unmarshal(validation.Body.Bytes(), &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(body.Errors) != 1 || !strings.Contains(body.Errors[0].Message, "expected number >= 1") || body.Errors[0].Location != "query.limit" {
		t.Fatalf("validation errors = %+v, want the minimum violation at query.limit", body.Errors)
	}

	notFound := api.Get("/x?limit=1")
	if notFound.Code != http.StatusNotFound || !strings.Contains(notFound.Body.String(), "unknown symbol") {
		t.Fatalf("404 = %d %s", notFound.Code, notFound.Body.String())
	}
	if logs.Len() != 0 {
		t.Fatalf("4xx must not be logged as errors: %q", logs.String())
	}
}
