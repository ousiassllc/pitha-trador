package admin

import "context"

// Start launches the MCP as a child process of the application when the
// stored settings allow it (see proc.Manager.Start). The errors are
// proc's: ErrAlreadyRunning, ErrAlreadyListening, ErrExeMissing,
// ErrDisabled, ErrNoKey, ErrSettingsUnusable, ErrAccessIncomplete,
// settings.ErrListenNotAllowed, ErrSpawnFailed, ErrExitedEarly.
func (s *Service) Start(ctx context.Context) error { return s.proc.Start(ctx) }

// Stop ends the process the application started; one started elsewhere is
// left alone (proc.ErrNotManaged).
func (s *Service) Stop(ctx context.Context) error { return s.proc.Stop(ctx) }

// Restart stops (if running) and starts the process, so saved settings and a
// regenerated key take effect.
func (s *Service) Restart(ctx context.Context) error { return s.proc.Restart(ctx) }

// AutoStart is the application's start-up hook (proc.Manager.AutoStart).
func (s *Service) AutoStart(ctx context.Context) { s.proc.AutoStart(ctx) }

// Shutdown is the application's exit hook: it stops the process and refuses
// later starts (proc.Manager.Shutdown).
func (s *Service) Shutdown() { s.proc.Shutdown() }
