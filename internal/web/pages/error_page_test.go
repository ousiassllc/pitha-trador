package pages_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/web/pages"
)

// The error screen shows the numeric status with a Japanese heading, never
// the English http.StatusText.
func TestErrorPage_HeadingIsJapaneseAndKeepsTheStatus(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusNotFound:            "404 ページが見つかりません",
		http.StatusInternalServerError: "500 サーバーでエラーが発生しました",
		http.StatusBadRequest:          "400 リクエストが正しくありません",
		http.StatusTeapot:              "418 エラー",
	} {
		var buf bytes.Buffer
		if err := pages.ErrorPage(status, "固定メッセージ").Render(context.Background(), &buf); err != nil {
			t.Fatalf("render %d: %v", status, err)
		}
		body := buf.String()
		if !strings.Contains(body, "<h1 class=\"text-2xl font-bold text-slate-900\">"+want+"</h1>") {
			t.Errorf("%d: h1 is not %q: %s", status, want, body)
		}
		if strings.Contains(body, http.StatusText(status)) {
			t.Errorf("%d: body still shows the English %q", status, http.StatusText(status))
		}
	}
}
