package researchwire_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/bootstrap"
	"github.com/ousiassllc/pitha-trador/internal/bootstrap/researchwire"
	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/config/tachibanasource"
	"github.com/ousiassllc/pitha-trador/internal/repository/market"
	"github.com/ousiassllc/pitha-trador/internal/repository/researchstatus"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// By default, and with the switches on but the 立花 credentials unset, the
// research collections never reach the broker: no login, no request, no
// connection, and the status says why. The broker selection is kabu throughout.
func TestCollectionsSendNothingWhileOffOrUnset(t *testing.T) {
	inSession := tt.AtJST(2026, 10, 8, 10, 0)
	for name, tc := range map[string]struct {
		authID string
		on     bool
		want   string // the quotes row's state ("" = no row at all)
	}{
		"off by default":        {authID: tt.AuthID, on: false, want: ""},
		"on, credentials unset": {authID: "", on: true, want: researchstatus.StateNotConfigured},
	} {
		t.Run(name, func(t *testing.T) {
			state, err := bootstrap.Run(bootstrap.Config{DBPath: filepath.Join(t.TempDir(), "pitha.db")})
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			t.Cleanup(func() { _ = state.Close() })
			fb := tt.New(t, tt.NewAutoClock(inSession))
			store := system.NewRuntimeSettingsRepository(state.DB)
			if tc.on {
				for _, k := range []string{tachibanasource.KeyResearchBarsEnabled, tachibanasource.KeyResearchQuotesEnabled} {
					if err := store.Set(context.Background(), k, "1", time.Now()); err != nil {
						t.Fatal(err)
					}
				}
			}
			w := researchwire.Build(researchwire.Deps{
				DB: state.DB, Settings: store,
				Secrets:   config.Secrets{TachibanaDemoAuthID: tc.authID},
				Broker:    config.BrokerSettings{Provider: config.BrokerKabu, Tachibana: fb.Settings()},
				DailyBars: market.NewDailyBarRepository(state.DB),
				InSession: func(time.Time) bool { return true },
			})
			var wg sync.WaitGroup
			ctx, cancel := context.WithCancel(context.Background())
			w.Hub.Start(ctx, &wg)
			if err := w.Bars.RunDue(ctx); err != nil {
				t.Fatalf("bars: %v", err)
			}
			if err := w.Quotes.Tick(ctx, inSession); err != nil {
				t.Fatalf("quotes: %v", err)
			}
			cancel()
			wg.Wait()
			if n := len(fb.Requests()); n != 0 {
				t.Errorf("the broker received %d requests, want 0", n)
			}
			st, ok, err := researchstatus.New(state.DB).Get(context.Background(), researchstatus.ComponentQuotes)
			if err != nil || ok != (tc.want != "") || st.State != tc.want {
				t.Errorf("quotes status = %+v, %v, %v; want state %q", st, ok, err, tc.want)
			}
			if !tc.on { // a default install writes nothing to any research table
				for _, table := range []string{"research_tachibana_status", "research_daily_bar_runs", "research_daily_bar_progress", "research_quote_samples"} {
					var n int
					if err := state.DB.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
						t.Errorf("%s has %d rows (err %v), want 0 by default", table, n, err)
					}
				}
			}
		})
	}
}
