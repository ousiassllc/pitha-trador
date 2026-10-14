package dataexport_test

import (
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/logging"
	"github.com/ousiassllc/pitha-trador/internal/service/dataexport"
)

// Secrets planted in every free-text / JSON column that can carry one; no
// archive may contain any of them (issue #770/#771 redaction).
// Redaction (issue #770/#771): no secret planted in any column of any table
// may appear anywhere in the archive, in either format.
func TestExport_NoSecretsInAnyFormat(t *testing.T) {
	e := newEnv(t)
	secrets := []string{secretToken, secretAPIKey, secretPassword, secretSecond, secretBearer}

	for _, format := range []dataexport.Format{dataexport.FormatCSV, dataexport.FormatJSONL} {
		_, files := export(t, e, dataexport.Request{Kinds: allKinds, Period: period, Format: format})
		for name, content := range files {
			for _, s := range secrets {
				if strings.Contains(content, s) {
					t.Errorf("%s (%s) contains secret %q:\n%s", name, format, s, content)
				}
			}
		}
		// The planted secrets sat in these columns, so the redaction is
		// exercised, not vacuous: the surrounding data is still there.
		if all := strings.Join(values(files), "\n"); !strings.Contains(all, logging.RedactedValue) {
			t.Errorf("%s: no redaction marker in archive; secrets were dropped, not masked", format)
		}
	}
}
