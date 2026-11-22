package opsettings

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/ousiassllc/pitha-trador/internal/config/researchrecordsettings"
	"github.com/ousiassllc/pitha-trador/internal/config/watchsettings"
)

// decodeText renders a stored threshold row as text.
func decodeText(key, raw string) (string, error) {
	switch kindOf(key) {
	case kindEntryQuality:
		return decodeString(key, raw)
	case kindBroker, kindSource, kindMetaLabel, kindLog, kindWatch:
		return decodeBroker(key, raw)
	}
	var f float64
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return "", fmt.Errorf("opsettings: stored %s=%q is not a JSON number: %w", key, raw, err)
	}
	return formatNumber(f), nil
}

func formatNumber(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func backupDirWarning(dir string) string {
	info, err := os.Stat(dir)
	switch {
	case err != nil:
		return "このディレクトリは現在利用できません（外付けドライブ未接続・パス誤りなど）。存在しない間、バックアップは失敗します。"
	case !info.IsDir():
		return "このパスはディレクトリではありません。バックアップは失敗します。"
	}
	return ""
}

// normalizeSymbols normalizes a symbol-list setting: the research FX pairs or
// index codes (research.fx.symbols, research.index.symbols) or the
// observation-only stock codes.
func normalizeSymbols(key, raw string) (string, error) {
	if researchrecordsettings.IsSymbolsKey(key) {
		return researchrecordsettings.Normalize(key, raw)
	}
	return watchsettings.NormalizeSymbols(raw)
}

// isResearchRecord reports a research record key (FX or index: the switch or the symbols).
func isResearchRecord(key string) bool { return researchrecordsettings.IsKey(key) }

// validateResearchRecord checks one stored research record value (FX or index).
func validateResearchRecord(key, encoded string) error {
	if err := researchrecordsettings.Validate(key, encoded); err != nil {
		return &InvalidValueError{Reason: err.Error()}
	}
	return nil
}
