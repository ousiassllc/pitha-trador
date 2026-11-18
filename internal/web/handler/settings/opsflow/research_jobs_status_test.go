package opsflow_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ousiassllc/pitha-trador/internal/config/researchjobsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
	"github.com/ousiassllc/pitha-trador/internal/service/researchjobstatus"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler/maintenance"
	"github.com/ousiassllc/pitha-trador/internal/web/handler/settings"
)

// writeCountingStore counts the writes of the store the status is read from.
type writeCountingStore struct {
	memRuntimeStore
	writes int
}

func (s *writeCountingStore) Set(ctx context.Context, key, value string, at time.Time) error {
	s.writes++
	return s.memRuntimeStore.Set(ctx, key, value, at)
}

func researchJobsRouter(store *writeCountingStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := settings.NewSettingsHandler(newFakeSecretsStore()).
		WithOperationalSettings(opsettings.New(store, opsStrategy(), "/data/logs")).
		WithResearchJobStatus(researchjobstatus.New(store))
	return settingsRouter(h)
}

// panelHTML is the research-jobs card's 夜間タスクの状態 panel out of the pages.
func panelHTML(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<section id="setting-group-derived-research-jobs".*?</section>`).FindString(body)
	if m == "" {
		t.Fatal("the research-jobs card has no 夜間タスクの状態 panel")
	}
	return m
}

var devNote = regexp.MustCompile(`#\d+|issue|Issue|PR `)

func TestResearchJobsCard_OffShowsOffDistinctFromFailureAndWritesNothing(t *testing.T) {
	store := &writeCountingStore{memRuntimeStore: memRuntimeStore{rows: map[string]string{}}}
	// A leftover failure of a task that is now off must not show as a failure.
	store.rows["system.maintenance.research_model.last_failure"] = `{"date":"2026-11-01","reason":"boom"}`
	panel := panelHTML(t, allPagesBody(t, researchJobsRouter(store)))
	if strings.Count(panel, "オフ（動いていません）") != 2 || strings.Contains(panel, "失敗") && !strings.Contains(panel, "失敗ではありません") {
		t.Errorf("both tasks must read オフ（動いていません）, not failed: %s", panel)
	}
	if strings.Contains(panel, "失敗しました") || strings.Contains(panel, "boom") || strings.Contains(panel, "注意:") {
		t.Errorf("off must not count as a failure: %s", panel)
	}
	if store.writes != 0 {
		t.Errorf("rendering the card wrote %d rows", store.writes)
	}
}

func TestResearchJobsCard_OnShowsLastSuccessAndFailureWithoutDevNotes(t *testing.T) {
	store := &writeCountingStore{memRuntimeStore: memRuntimeStore{rows: map[string]string{
		researchjobsettings.KeyEvalEnabled:  "1",
		researchjobsettings.KeyModelEnabled: "1",
	}}}
	r := maintenance.NewRunner(store, maintenance.WithClock(func() time.Time { return time.Date(2026, 11, 19, 17, 0, 0, 0, time.Local) }))
	r.Add(maintenance.Task{Name: researchjobstatus.EvalTask, Label: "eval", TrackFailure: true, Run: func(context.Context) error { return nil }})
	r.Add(maintenance.Task{Name: researchjobstatus.ModelTask, Label: "model", TrackFailure: true, Run: func(context.Context) error {
		return errors.New("nightlyjobs: research model: too few\nrows to train")
	}})
	r.CatchUp(t.Context())

	body := allPagesBody(t, researchJobsRouter(store))
	panel := panelHTML(t, body)
	for _, want := range []string{"最終成功日 2026-11-19", "直近の実行が失敗しました（2026-11-19）", "理由: nightlyjobs: research model: too few rows to train", "取引の判断や昇格には使われません"} {
		if !strings.Contains(panel, want) {
			t.Errorf("the panel must contain %q: %s", want, panel)
		}
	}
	if strings.Contains(panel, "オフ（動いていません）") {
		t.Errorf("both switches are on: %s", panel)
	}
	if m := devNote.FindString(panel); m != "" {
		t.Errorf("the panel must carry no dev notes, found %q: %s", m, panel)
	}
}

func TestResearchJobsCard_UnreadableStatusSaysSoAndKeepsTheSwitches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &writeCountingStore{memRuntimeStore: memRuntimeStore{rows: map[string]string{}}}
	h := settings.NewSettingsHandler(newFakeSecretsStore()).
		WithOperationalSettings(opsettings.New(store, opsStrategy(), "/data/logs")).
		WithResearchJobStatus(failingStatus{})
	body := allPagesBody(t, settingsRouter(h))
	if panel := panelHTML(t, body); !strings.Contains(panel, "夜間タスクの状態を読み取れませんでした") || strings.Contains(panel, "injected") {
		t.Errorf("the panel must say it is unreadable without the raw error: %s", panel)
	}
	rowHTML(t, body, researchjobsettings.KeyEvalEnabled)
}

type failingStatus struct{}

func (failingStatus) Tasks(context.Context) ([]researchjobstatus.Task, error) {
	return nil, errors.New("injected read failure")
}

// Without the status source (router tests) the card shows its switches only.
func TestResearchJobsCard_WithoutStatusSourceHasNoPanel(t *testing.T) {
	engine, _ := opsRouter()
	body := allPagesBody(t, engine)
	if strings.Contains(body, "setting-group-derived-research-jobs") {
		t.Error("no panel without a status source")
	}
	rowHTML(t, body, researchjobsettings.KeyEvalEnabled)
}
