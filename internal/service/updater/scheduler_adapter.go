package updater

import "context"

// Quitter lets SchedulerAdapter trigger the caller's actual process-level
// restart once CheckForUpdate has downloaded and verified a newer
// installer, without this package importing Wails itself (package doc
// comment): cmd/desktop's *App implements it via runtime.Quit against the
// ctx its own Wails OnStartup hook received.
type Quitter interface {
	QuitForUpdate(installerPath string)
}

// SchedulerAdapter adapts Checker to internal/service/scheduler's
// UpdateChecker interface (CheckForUpdate(ctx) error), forwarding a
// verified installer's path to Quitter instead of returning it - the
// scheduler cron trigger that calls this only logs a returned error, so a
// non-Ready, no-error Result being silently discarded here is intentional:
// Checker.CheckForUpdate already logs why it declined (already up to
// date, or the safety gate rejected it this tick).
type SchedulerAdapter struct {
	Checker *Checker
	Quitter Quitter
}

// CheckForUpdate implements internal/service/scheduler.UpdateChecker.
func (a SchedulerAdapter) CheckForUpdate(ctx context.Context) error {
	result, err := a.Checker.CheckForUpdate(ctx)
	if err != nil {
		return err
	}
	if result.Ready {
		a.Quitter.QuitForUpdate(result.InstallerPath)
	}
	return nil
}

// Status returns Checker's most recent check outcome (issue #76), for the
// UI's update banner/Settings panel.
func (a SchedulerAdapter) Status() Status {
	return a.Checker.Status()
}
