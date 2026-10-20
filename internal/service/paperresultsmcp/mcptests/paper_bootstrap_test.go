package mcptests

import (
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/domain"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// issue #798: PaperBootstrapUse reports, from trade_signals alone, whether the
// Paper-only bootstrap entry took effect in a range (the base of the
// health_today field, #808). It matches the policy_version the Policy Engine
// writes - alone, or followed by an applied proposal's "+<version>" - and never
// a lookalike.
func TestPaperBootstrapUse(t *testing.T) {
	_, w := testkit.NewMigratedDB(t)
	testkit.MustExec(t, w, `INSERT INTO instruments (id, symbol, name, market) VALUES (1, '7203', 'n', 'TSE')`)
	for _, s := range []struct{ at, version string }{
		{"2026-10-01T00:01:00Z", domain.PolicyVersionPaperBootstrap},
		{"2026-10-01T00:05:00Z", domain.PolicyVersionPaperBootstrap + "+sol-12"},
		{"2026-10-01T00:03:00Z", "policy-v2"},                        // a model decision
		{"2026-10-01T00:04:00Z", "policy-vbx"},                       // a lookalike
		{"2026-10-02T00:00:00Z", domain.PolicyVersionPaperBootstrap}, // outside the range
	} {
		testkit.MustExec(t, w, `INSERT INTO trade_signals (instrument_id, symbol, timestamp, direction, policy_version, risk_passed)
			VALUES (1, '7203', ?, 'NONE', ?, 0)`, testkit.Stored(t, s.at), s.version)
	}
	day := func(d int) time.Time { return time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC) }

	got, err := query.PaperBootstrapUse(t.Context(), w, query.Range{From: day(1), To: day(2)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != query.PaperBootstrapInUse || got.Signals != 2 || got.LastSignalAt == nil || *got.LastSignalAt != "2026-10-01T00:05:00Z" {
		t.Fatalf("in range = %+v, want in_use, 2 signals, last 2026-10-01T00:05:00Z", got)
	}

	none, err := query.PaperBootstrapUse(t.Context(), w, query.Range{From: day(3), To: day(4)})
	if err != nil {
		t.Fatal(err)
	}
	if none.Code != query.PaperBootstrapNotInUse || none.Signals != 0 || none.LastSignalAt != nil {
		t.Fatalf("empty range = %+v, want not_in_use", none)
	}
}
