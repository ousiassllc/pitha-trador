package mcpset

import (
	"context"
	"database/sql"

	"github.com/ousiassllc/pitha-trador/internal/config/mcpwritesettings"
	"github.com/ousiassllc/pitha-trador/internal/repository/system"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/experiment"
)

// SetWithExperiment is Set plus one tuning experiment for the very change
// (issue #1088): kind setting, name key, before / after the key's effective
// value before and after the save, audit_id the audit row of this change,
// started_at the write time. The setting, its audit row and the experiment are
// one transaction: if any step fails - an invalid value, an empty or too long
// hypothesis, a save that left the value as it was (before == after: an
// experiment that changed nothing is refused like experiment_create does) -
// nothing is stored.
//
// The checks run in Set's order (the whitelist, the write switch, the value) and
// the hypothesis is checked right after the write switch, before anything is
// stored. The returned Entry is the audit row, the Experiment the new running
// experiment as experiment_create returns it.
func (w *Writer) SetWithExperiment(ctx context.Context, client, key, raw, hypothesis string) (Entry, experiment.Experiment, error) {
	if !mcpwritesettings.IsAllowed(key) {
		return Entry{}, experiment.Experiment{}, ErrKeyNotAllowed
	}
	var (
		entry Entry
		exp   experiment.Experiment
	)
	err := runInTx(ctx, w.db, "set with experiment transaction", func(tx *sql.Tx) error {
		store := system.NewRuntimeSettingsRepositoryOn(tx)
		on, err := WriteEnabled(ctx, store)
		if err != nil {
			return err
		}
		if !on {
			return ErrWriteDisabled
		}
		now := w.now()
		// The hypothesis (and the shape of the experiment) is validated with
		// placeholder values first, so a bad hypothesis stops before the setting
		// is touched; the real before / after are checked once they are known.
		args := experiment.Args{Hypothesis: hypothesis, Changes: []experiment.Change{{Kind: experiment.KindSetting, Name: key, Before: "0", After: "1"}}}
		if _, err := args.Validate(now); err != nil {
			return err
		}
		saved, before, after, err := w.saveAndAudit(ctx, store, tx, client, key, raw)
		if err != nil {
			return err
		}
		entry = saved
		// A key that was never saved and defaults to empty has no value to
		// record; an experiment's before cannot be empty, so it reads UnsetValue
		// there (the setting and its audit row keep their own values).
		beforeText := before.Current
		if beforeText == "" {
			beforeText = experiment.UnsetValue
		}
		args.Changes[0].Before, args.Changes[0].After, args.AuditID = beforeText, after.Current, &entry.ID
		req, err := args.Validate(now)
		if err != nil {
			return err
		}
		exp, err = experiment.Insert(ctx, tx, now, experiment.ActorMCP, client, req)
		return err
	})
	if err != nil {
		return Entry{}, experiment.Experiment{}, err
	}
	return entry, exp, nil
}
