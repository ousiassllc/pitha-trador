package performance

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func ginContext(query string) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/performance?"+query, nil)
	return c
}

// The presets are the last N JST days including today, from the JST date of
// now (03:00 UTC of the 20th is noon JST; 16:00 UTC is already the 21st JST).
func TestParseEvaluationPeriod_PresetsFollowTheJSTDay(t *testing.T) {
	for _, tc := range []struct {
		name string
		now  time.Time
		want [3][2]string // from/to of 直近7/30/90日
	}{
		{"noon JST", time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC), [3][2]string{
			{"2026-10-14", "2026-10-20"}, {"2026-09-21", "2026-10-20"}, {"2026-07-23", "2026-10-20"}}},
		{"after midnight JST", time.Date(2026, 10, 20, 16, 0, 0, 0, time.UTC), [3][2]string{
			{"2026-10-15", "2026-10-21"}, {"2026-09-22", "2026-10-21"}, {"2026-07-24", "2026-10-21"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _, _ := parseEvaluationPeriod(ginContext(""), tc.now)
			if len(p.Links) != 4 || p.Links[0].Label != "全期間" || p.Links[0].Href != "/performance" || !p.Links[0].Current {
				t.Fatalf("links = %+v, want 全期間 current then 3 presets", p.Links)
			}
			for i, w := range tc.want {
				href := "/performance?eval_from=" + w[0] + "&eval_to=" + w[1]
				if p.Links[i+1].Href != href || p.Links[i+1].Current {
					t.Errorf("preset %d = %+v, want href %q not current", i, p.Links[i+1], href)
				}
			}
		})
	}
}

func TestParseEvaluationPeriod_MatchingPresetIsCurrent(t *testing.T) {
	now := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	p, r, msg := parseEvaluationPeriod(ginContext("eval_from=2026-10-14&eval_to=2026-10-20"), now)
	if msg != "" || p.Links[0].Current || !p.Links[1].Current || p.Links[2].Current {
		t.Errorf("msg %q links %+v, want only 直近7日 current", msg, p.Links)
	}
	if want := time.Date(2026, 10, 21, 0, 0, 0, 0, jst); !r.To.Equal(want) {
		t.Errorf("To = %v, want %v (end day inclusive)", r.To, want)
	}
}
