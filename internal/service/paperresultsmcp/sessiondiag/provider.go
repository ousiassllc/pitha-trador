package sessiondiag

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/budget"
)

// ProviderUnknown is BrokerProvider when the stored broker selection could not
// be read: watch_lists is then judged as before (an empty day is
// watch_list_empty), because nothing says it does not apply.
const ProviderUnknown = "unknown"

// readProvider reads the broker selection (config.KeyBrokerProvider, issue
// #914) the way the app resolves it at start (opsettings.LoadBroker): a missing
// row, or a stored value that is not a JSON string naming a known broker, is
// the default (kabu). It reads that one key only (the setting holds no secret;
// the other broker keys include key-file paths and are never read here).
// A read that fails is ProviderUnknown, never an error: the diagnosis of the
// days does not depend on it; a read that ran out of the soft time budget is
// also recorded as incomplete (Diagnostics.TimedOut).
func (r *reader) readProvider() string {
	var raw string
	err := r.q.QueryRowContext(r.soft, `SELECT value FROM runtime_settings WHERE key = ?`, config.KeyBrokerProvider).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return config.BrokerSettingDefault(config.KeyBrokerProvider)
	}
	if err != nil {
		if budget.Spent(r.ctx, r.soft, err) {
			_, _ = r.try("broker_provider", err)
		} else {
			slog.Warn("paperresultsmcp: session_diagnostics could not read the broker selection; judging the watch lists as before", "error", err)
		}
		return ProviderUnknown
	}
	var text string
	if err := json.Unmarshal([]byte(raw), &text); err != nil {
		return config.BrokerSettingDefault(config.KeyBrokerProvider)
	}
	normalized, err := config.NormalizeBrokerSetting(config.KeyBrokerProvider, text)
	if err != nil {
		return config.BrokerSettingDefault(config.KeyBrokerProvider)
	}
	return normalized.(string)
}

// watchListObservation is the observation of a day's watch_lists rows, or ""
// for none. Only the 立花 adapter writes them (tachibanawatch.Decider); with
// kabu the table stays empty by design, so an empty day is
// ObsWatchListNotApplicable, not ObsWatchListEmpty (the kabu watch list lives
// in memory and is not stored).
func watchListObservation(provider string, l *WatchListDay) string {
	if l == nil || l.Entries != 0 {
		return ""
	}
	if provider == config.BrokerKabu {
		return ObsWatchListNotApplicable
	}
	return ObsWatchListEmpty
}
