// Package researchjobsettings holds the Settings-screen switches and retentions of
// the research-only jobs of issues #1089, #1092 and #1094: the daily record of the
// Jev evaluation (table jev_evaluation_daily) and the nightly walk-forward training of
// the feature-only research model (tables research_model_runs / research_model_predictions),
// and the switch of the training-dataset export tool.
//
// The keys live in runtime_settings (docs/architecture/er.md §runtime_settings); there is
// no yaml or environment source and they are not on the Paper results MCP's settings_set
// whitelist. Both jobs write only their own research tables and never a trading input,
// but they are writes the trading process makes, so all three are OFF by default: while
// nothing is stored (or the stored value is damaged) the jobs write nothing.
package researchjobsettings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
)

// Keys of the runtime_settings rows.
const (
	// KeyEvalEnabled switches the daily Jev evaluation record: the JSON integer 1 or 0.
	KeyEvalEnabled = "research.eval.enabled"
	// KeyEvalRetentionDays is how many JST days of jev_evaluation_daily rows are kept.
	KeyEvalRetentionDays = "research.eval.retention_days"
	// KeyModelEnabled switches the nightly research-model training: the JSON integer 1 or 0.
	KeyModelEnabled = "research.model.enabled"
	// KeyModelRetentionDays is how many JST days of research model runs and predictions are kept.
	KeyModelRetentionDays = "research.model.retention_days"
	// KeyExportEnabled switches the training-dataset export (issue #1094): while off, the MCP's
	// training_dataset_export tool writes nothing and says so. The JSON integer 1 or 0.
	KeyExportEnabled = "research.export.enabled"
)

// Defaults and bounds, in JST days. Both switches default to off.
const (
	DefaultEnabled            = false
	MinRetentionDays          = 1
	MaxRetentionDays          = 3650
	DefaultEvalRetentionDays  = 365
	DefaultModelRetentionDays = 90
)

// Keys returns every runtime_settings key of this package.
func Keys() []string {
	return []string{KeyEvalEnabled, KeyEvalRetentionDays, KeyModelEnabled, KeyModelRetentionDays, KeyExportEnabled}
}

// IsKey reports whether key is one of Keys.
func IsKey(key string) bool {
	return slices.Contains(Keys(), key)
}

// IsSwitchKey reports whether key is one of the on/off switches.
func IsSwitchKey(key string) bool {
	return key == KeyEvalEnabled || key == KeyModelEnabled || key == KeyExportEnabled
}

// DefaultText is the value a key shows while nothing is stored.
func DefaultText(key string) string {
	switch key {
	case KeyEvalRetentionDays:
		return fmt.Sprint(DefaultEvalRetentionDays)
	case KeyModelRetentionDays:
		return fmt.Sprint(DefaultModelRetentionDays)
	}
	return "0"
}

// Validate checks the JSON-encoded value of key: 0 or 1 for a switch, a whole number of
// MinRetentionDays..MaxRetentionDays days for a retention.
func Validate(key, jsonValue string) error {
	var n *float64 // a pointer, so JSON null is told from 0
	if err := json.Unmarshal([]byte(jsonValue), &n); err != nil || n == nil || math.IsNaN(*n) || *n != math.Trunc(*n) {
		return fmt.Errorf("researchjobsettings: runtime setting %s=%q must be a whole number", key, jsonValue)
	}
	switch {
	case IsSwitchKey(key) && *n != 0 && *n != 1:
		return fmt.Errorf("researchjobsettings: runtime setting %s=%q must be 0（無効）または 1（有効）", key, jsonValue)
	case !IsSwitchKey(key) && (*n < MinRetentionDays || *n > MaxRetentionDays):
		return fmt.Errorf("researchjobsettings: runtime setting %s=%q must be %d〜%d days", key, jsonValue, MinRetentionDays, MaxRetentionDays)
	}
	return nil
}

// Store is the runtime_settings read access Load needs; *system.RuntimeSettingsRepository implements it.
type Store interface {
	Get(ctx context.Context, key string) (value string, ok bool, err error)
}

// Job is the effective state of one job's two keys.
type Job struct {
	Enabled       bool
	RetentionDays int
}

// Eval loads the settings of the daily evaluation record.
func Eval(ctx context.Context, store Store) (Job, error) {
	return load(ctx, store, KeyEvalEnabled, KeyEvalRetentionDays, DefaultEvalRetentionDays)
}

// Model loads the settings of the nightly research-model training.
func Model(ctx context.Context, store Store) (Job, error) {
	return load(ctx, store, KeyModelEnabled, KeyModelRetentionDays, DefaultModelRetentionDays)
}

// load reads one job's switch and retention. A key with nothing stored, or a stored row
// that does not validate, is logged if damaged and counts as its default, so a fault can
// neither switch a job on nor shorten a retention. Only a failed store read is an error:
// the caller must not guess then.
func load(ctx context.Context, store Store, enabledKey, retentionKey string, defaultDays int) (Job, error) {
	j := Job{Enabled: DefaultEnabled, RetentionDays: defaultDays}
	for _, key := range []string{enabledKey, retentionKey} {
		raw, ok, err := store.Get(ctx, key)
		if err != nil {
			return Job{}, fmt.Errorf("researchjobsettings: read %s: %w", key, err)
		}
		if !ok {
			continue
		}
		if err := Validate(key, raw); err != nil {
			slog.Warn("researchjobsettings: ignoring invalid stored setting; the default applies", "key", key, "error", err)
			continue
		}
		var n int
		_ = json.Unmarshal([]byte(raw), &n) // Validate accepted a whole number
		if key == enabledKey {
			j.Enabled = n == 1
		} else {
			j.RetentionDays = n
		}
	}
	return j, nil
}
