package updatetool

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository/updatecontrol"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/query"
	"github.com/ousiassllc/pitha-trador/internal/service/updater"
	"github.com/ousiassllc/pitha-trador/internal/version"
)

// view is update_status's data (and the base of update_check_now's). It holds
// fixed codes and fixed wording only: never LastError, an installer path, a
// URL or a key.
type view struct {
	// Unavailable: the build has no updater (cmd/server), or the application is not running.
	Unavailable       bool   `json:"unavailable"`
	UnavailableReason string `json:"unavailable_reason,omitempty"`

	CurrentVersion string `json:"current_version"`
	LatestVersion  string `json:"latest_version,omitempty"`
	CheckedAt      string `json:"checked_at,omitempty"`

	Available bool `json:"available"`
	Blocked   bool `json:"blocked"`
	Ready     bool `json:"ready"`
	DevBuild  bool `json:"dev_build"`
	NoRelease bool `json:"no_release"`

	BlockedKind   string `json:"blocked_kind,omitempty"`
	BlockedReason string `json:"blocked_reason,omitempty"`
	ErrorKind     string `json:"error_kind,omitempty"`
	ErrorReason   string `json:"error_reason,omitempty"`

	LastApply *applyAudit `json:"last_apply,omitempty"`
}

// applyAudit is the newest update_apply request, from the audit.
type applyAudit struct {
	RequestedAt string `json:"requested_at"`
	Actor       string `json:"actor"`
	Client      string `json:"client,omitempty"`
	Result      string `json:"result"` // accepted / deferred / expired, or pending while the application has not answered
	Reason      string `json:"reason,omitempty"`
}

// readView reads the published status (and the apply audit) as q sees it.
func readView(ctx context.Context, q query.Q, now time.Time) (view, error) {
	st, ok, err := updatecontrol.ReadStatus(ctx, q)
	if err != nil {
		return view{}, err
	}
	if !ok || now.Sub(st.HeartbeatAt) > updatecontrol.StaleAfter {
		return unavailableView(), nil
	}
	v := view{
		CurrentVersion: st.CurrentVersion, LatestVersion: st.LatestVersion,
		Available: st.Available, Blocked: st.Blocked, Ready: st.Ready, DevBuild: st.DevBuild, NoRelease: st.NoRelease,
		BlockedKind: st.BlockedKind, ErrorKind: st.ErrorKind,
	}
	if v.CurrentVersion == "" {
		v.CurrentVersion = version.Version
	}
	if st.CheckedAt != nil {
		v.CheckedAt = st.CheckedAt.UTC().Format(time.RFC3339)
	}
	if st.Blocked {
		v.BlockedReason = updater.BlockedReason(updater.BlockKind(st.BlockedKind))
	}
	if st.ErrorKind != "" {
		v.ErrorReason = updater.ErrorReason(updater.ErrorKind(st.ErrorKind))
	}
	last, ok, err := updatecontrol.LastApply(ctx, q)
	if err != nil {
		return view{}, err
	}
	if ok {
		v.LastApply = &applyAudit{
			RequestedAt: last.RequestedAt.UTC().Format(time.RFC3339), Actor: updatecontrol.ActorMCP, Client: last.Client,
			Result: last.Outcome, Reason: last.Reason,
		}
		if !last.Done() {
			v.LastApply.Result = "pending"
		}
	}
	return v, nil
}
