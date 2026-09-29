package scheduler_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/repository"
	"github.com/ousiassllc/pitha-trador/internal/service/scheduler"
)

type fakeBackuper struct {
	calls int
	err   error
}

func (f *fakeBackuper) Backup(ctx context.Context) error {
	f.calls++
	return f.err
}

func TestScheduler_BackupDatabase_NoOpWithoutBackuper(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(repository.NewJobRepository(db), repository.NewInstrumentRepository(db))
	if err := s.BackupDatabase(context.Background()); err != nil {
		t.Fatalf("BackupDatabase: %v (want nil, no WithDatabaseBackuper configured)", err)
	}
}

func TestScheduler_BackupDatabase_CallsBackuperAndPropagatesError(t *testing.T) {
	db := newTestDB(t)
	jobs := repository.NewJobRepository(db)
	instruments := repository.NewInstrumentRepository(db)

	ok := &fakeBackuper{}
	s := scheduler.New(jobs, instruments, scheduler.WithDatabaseBackuper(ok))
	if err := s.BackupDatabase(context.Background()); err != nil {
		t.Fatalf("BackupDatabase: %v", err)
	}
	if ok.calls != 1 {
		t.Fatalf("backuper.calls = %d, want 1", ok.calls)
	}

	failing := &fakeBackuper{err: errors.New("disk full")}
	s = scheduler.New(jobs, instruments, scheduler.WithDatabaseBackuper(failing))
	if err := s.BackupDatabase(context.Background()); err == nil {
		t.Fatal("BackupDatabase should propagate the backuper's error")
	}
}

func TestScheduler_Start_RegistersDailyDatabaseBackupTrigger(t *testing.T) {
	db := newTestDB(t)
	s := scheduler.New(repository.NewJobRepository(db), repository.NewInstrumentRepository(db),
		scheduler.WithDatabaseBackuper(&fakeBackuper{}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := s.Start(ctx, 24*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop()
}

// settingsStub is an in-memory maintenance.State; mutex-guarded since Start's
// catch-up goroutine writes while tests poll.
type settingsStub struct {
	mu sync.Mutex
	m  map[string]string
}

func (s *settingsStub) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return v, ok, nil
}

func (s *settingsStub) Set(_ context.Context, key, value string, _ time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = value
	return nil
}
func (s *settingsStub) value(key string) (v string) {
	v, _, _ = s.Get(context.Background(), key)
	return
}

const backupStateKey = "system.maintenance.database_backup.last_success_date"

func TestScheduler_Start_CatchesUpMissedBackupImmediately(t *testing.T) {
	db := newTestDB(t)
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	state := &settingsStub{m: map[string]string{backupStateKey: `"` + yesterday + `"`}}
	b := &fakeBackuper{}
	s := scheduler.New(repository.NewJobRepository(db), repository.NewInstrumentRepository(db),
		scheduler.WithDatabaseBackuper(b), scheduler.WithMaintenanceState(state))
	if err := s.Start(context.Background(), 24*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && state.value(backupStateKey) == `"`+yesterday+`"` {
		time.Sleep(10 * time.Millisecond)
	}
	if got, want := state.value(backupStateKey), `"`+time.Now().Format("2006-01-02")+`"`; got != want {
		t.Fatalf("last success = %s, want %s (catch-up did not run right after Start)", got, want)
	}
}

func TestScheduler_Start_SkipsBackupAlreadyDoneToday(t *testing.T) {
	db := newTestDB(t)
	today := time.Now().Format("2006-01-02")
	state := &settingsStub{m: map[string]string{backupStateKey: `"` + today + `"`}}
	b := &fakeBackuper{}
	s := scheduler.New(repository.NewJobRepository(db), repository.NewInstrumentRepository(db),
		scheduler.WithDatabaseBackuper(b), scheduler.WithMaintenanceState(state))
	if err := s.Start(context.Background(), 24*time.Hour); err != nil {
		t.Fatalf("Start: %v", err)
	}
	s.Stop() // waits for the immediate catch-up goroutine
	if b.calls != 0 {
		t.Fatalf("backuper.calls = %d, want 0 (already succeeded today)", b.calls)
	}
}
