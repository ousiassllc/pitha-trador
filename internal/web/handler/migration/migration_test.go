package migration

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle"
	"github.com/ousiassllc/pitha-trador/internal/service/migratebundle/importflow"
)

// Every refusal the import flow can produce is a 4xx with an operator-facing
// Japanese message; only an unknown error is a 500 with the fixed message.
func TestClassify_MapsImportErrorsToStatusAndJapaneseDetail(t *testing.T) {
	tests := []struct {
		err    error
		status int
		detail string
	}{
		{migratebundle.ErrSchemaTooNew, http.StatusUnprocessableEntity, "より新しいバージョン"},
		{migratebundle.ErrUnsupportedFormat, http.StatusUnprocessableEntity, "形式のバージョン"},
		{migratebundle.ErrNotBundle, http.StatusUnprocessableEntity, "PC 移行ファイルではありません"},
		{migratebundle.ErrChecksum, http.StatusUnprocessableEntity, "何も変更していません"},
		{migratebundle.ErrCorrupt, http.StatusUnprocessableEntity, "何も変更していません"},
		{migratebundle.ErrNotSecretFree, http.StatusUnprocessableEntity, "秘密情報"},
		{importflow.ErrUnknownToken, http.StatusConflict, "もう一度ファイルを選んで"},
		{importflow.ErrReplaceNotConfirmed, http.StatusConflict, "置き換えることの確認"},
		{errors.New("disk exploded: /secret/path"), http.StatusInternalServerError, "取り込みに失敗しました"},
	}
	for _, tc := range tests {
		t.Run(tc.err.Error(), func(t *testing.T) {
			status, detail := classify(fmt.Errorf("wrapped: %w", tc.err))
			if status != tc.status || !strings.Contains(detail, tc.detail) {
				t.Errorf("classify = %d %q, want %d containing %q", status, detail, tc.status, tc.detail)
			}
			if strings.Contains(detail, "/secret/path") {
				t.Errorf("detail leaks the cause: %q", detail)
			}
		})
	}
}
