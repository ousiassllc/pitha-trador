package virtualpapertests

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"

	repo "github.com/ousiassllc/pitha-trador/internal/repository/virtualpaper"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/testkit"
)

// seedMark stores a day mark of the given status, the way the recorder does.
func seedMark(t *testing.T, w *sql.DB, id int64, day, status string) {
	t.Helper()
	d := repo.Day{ProfileID: id, Day: day, Status: status}
	if status == repo.DayFailed {
		d.Error = "replay failed"
	}
	if err := repo.RecordDay(t.Context(), w, d, nil, at(day, 17, 0)); err != nil {
		t.Fatal(err)
	}
}

func markStatus(t *testing.T, w *sql.DB, id int64, day string) string {
	t.Helper()
	d, err := repo.GetDay(t.Context(), w, id, day)
	if err != nil {
		return ""
	}
	return d.Status
}

type rerunResult struct {
	ProfileID int64  `json:"profile_id"`
	Day       string `json:"day"`
	Reset     bool   `json:"reset"`
}

// The tool takes only the failed day's mark away (so the next daily record replays
// it) and writes nothing but virtual_paper_days: no order, position, signal,
// setting or audit row appears.
func TestRerunFailed_ResetsOnlyTheFailedMark(t *testing.T) {
	srv, w := newServer(t)
	p := srv.create(t, map[string]any{"name": "p"}).ID
	seedMark(t, w, p, "2026-10-14", repo.DayFailed)
	seedMark(t, w, p, "2026-10-15", repo.DayFailed)
	seedMark(t, w, p, "2026-10-16", repo.DayDone)

	var out rerunResult
	srv.call(t, "virtual_paper_rerun_failed", map[string]any{"profile_id": p, "day": "2026-10-14"}).data(t, "virtual_paper_rerun_failed", &out)
	if out.ProfileID != p || out.Day != "2026-10-14" || !out.Reset {
		t.Fatalf("result = %+v", out)
	}
	if st := markStatus(t, w, p, "2026-10-14"); st != "" {
		t.Errorf("the reset day's mark = %q, want none (pending)", st)
	}
	if st := markStatus(t, w, p, "2026-10-15"); st != repo.DayFailed {
		t.Errorf("another failed day = %q, want it kept (nothing reruns unless asked)", st)
	}
	if st := markStatus(t, w, p, "2026-10-16"); st != repo.DayDone {
		t.Errorf("the done day = %q, want it kept", st)
	}
	for _, tbl := range []string{"paper_orders", "positions", "trade_signals", "runtime_settings", "mcp_settings_audit", "virtual_paper_trades"} {
		if n := count(t, w, `SELECT COUNT(*) FROM `+tbl); n != 0 {
			t.Errorf("%s has %d rows after the reset, want 0", tbl, n)
		}
	}
}

func TestRerunFailed_Refusals(t *testing.T) {
	srv, w := newServer(t)
	p := srv.create(t, map[string]any{"name": "p"}).ID
	stopped := srv.create(t, map[string]any{"name": "stopped"}).ID
	seedMark(t, w, p, "2026-10-12", repo.DayDone)
	seedMark(t, w, p, "2026-10-13", repo.DayEmpty)
	seedMark(t, w, p, "2026-10-01", repo.DayFailed) // older than the 10-day back-fill window
	seedMark(t, w, stopped, "2026-10-14", repo.DayFailed)
	srv.call(t, "virtual_paper_profile_stop", map[string]any{"profile_id": stopped})
	before := count(t, w, `SELECT COUNT(*) FROM virtual_paper_days`)

	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"done day", map[string]any{"profile_id": p, "day": "2026-10-12"}, "day"},
		{"empty day", map[string]any{"profile_id": p, "day": "2026-10-13"}, "day"},
		{"unknown day", map[string]any{"profile_id": p, "day": "2026-10-14"}, "day"},
		{"not a date", map[string]any{"profile_id": p, "day": "secret-value"}, "day"},
		{"too old", map[string]any{"profile_id": p, "day": "2026-10-01"}, "day"},
		{"unknown profile", map[string]any{"profile_id": 999, "day": "2026-10-14"}, "profile_id"},
		{"stopped profile", map[string]any{"profile_id": stopped, "day": "2026-10-14"}, "profile_id"},
		{"missing day", map[string]any{"profile_id": p}, ""},
		{"missing profile", map[string]any{"day": "2026-10-14"}, ""},
		{"zero profile", map[string]any{"profile_id": 0, "day": "2026-10-14"}, ""},
	}
	for _, c := range cases {
		res := srv.call(t, "virtual_paper_rerun_failed", c.args)
		detail := res.wantError(t, http.StatusUnprocessableEntity, paperresultsmcp.CodeInvalidArgument)
		if !strings.Contains(detail, c.want) {
			t.Errorf("%s: detail %q does not name %q", c.name, detail, c.want)
		}
		if strings.Contains(string(res.body), "secret-value") {
			t.Errorf("%s: the error echoes the argument: %s", c.name, res.body)
		}
	}
	if n := count(t, w, `SELECT COUNT(*) FROM virtual_paper_days`); n != before {
		t.Errorf("%d day marks after the refusals, want %d (nothing written)", n, before)
	}
	for _, tbl := range []string{"virtual_paper_days"} {
		if st := markStatus(t, w, p, "2026-10-12"); st != repo.DayDone {
			t.Errorf("%s: done mark = %q after a refusal", tbl, st)
		}
	}
}

// While the write switch is off the tool is refused with write_disabled and the
// failed mark stays.
func TestRerunFailed_WriteSwitchOff(t *testing.T) {
	srv, w := newServer(t)
	p := srv.create(t, map[string]any{"name": "p"}).ID
	seedMark(t, w, p, "2026-10-14", repo.DayFailed)
	testkit.MustExec(t, w, `INSERT INTO runtime_settings (key, value, updated_at) VALUES ('mcp.settings_write_enabled', '0', 't')`)

	srv.call(t, "virtual_paper_rerun_failed", map[string]any{"profile_id": p, "day": "2026-10-14"}).wantError(t, http.StatusForbidden, paperresultsmcp.CodeWriteDisabled)
	if st := markStatus(t, w, p, "2026-10-14"); st != repo.DayFailed {
		t.Errorf("mark = %q while the switch was off, want failed", st)
	}
}
