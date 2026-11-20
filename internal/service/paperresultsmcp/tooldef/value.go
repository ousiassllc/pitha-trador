package tooldef

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/mcpset"
)

// PaperCapital is the Paper capital for the return-rate calculations: the
// Settings value (risk.paper.initial_capital_jpy in runtime_settings, read
// through the read-only connection) when one is stored, otherwise the
// risk.yaml value the process started with.
func (e Env) PaperCapital(ctx context.Context) (float64, error) {
	return mcpset.PaperCapitalJPY(ctx, e.Q, e.CapitalJPY)
}

// ValueText turns a JSON string or number into the text the Settings screen
// would receive. Booleans, null, arrays and objects are not values.
func ValueText(raw json.RawMessage) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", false
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return "", false
		}
		return s, true
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			return "", false
		}
		return strconv.FormatFloat(f, 'f', -1, 64), true
	}
	return "", false
}
