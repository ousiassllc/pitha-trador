// Package handlerflow_test holds the policy.Handler (HandleJob) tests. They
// only use policy's exported API and live in their own directory to keep
// internal/service/policy under the linterly line budget (#1173). The
// helpers below are this package's own (sibling test packages do not import
// each other).
package handlerflow_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/policy"
)

func testThresholds() policy.Thresholds {
	return policy.Thresholds{
		Policy: config.PolicyConfig{
			Long: config.PolicyDirectionThresholds{
				MinProbability: 0.68, MinEntryQuality: "strong",
				MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
			},
			Short: config.PolicyDirectionThresholds{
				MinProbability: 0.68, MinEntryQuality: "strong",
				MinContinuationProbability: 0.60, MaxToxicFlow: 0.35, MaxLiquidityStressed: 0.25,
			},
		},
		MaxSpreadBps:     50,
		MinTurnover5mJPY: 3_000_000,
	}
}

func ptr[T any](v T) *T { return &v }

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func decodeLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if raw == "" {
			continue
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("log line is not valid JSON: %v\nline: %s", err, raw)
		}
		lines = append(lines, decoded)
	}
	return lines
}
