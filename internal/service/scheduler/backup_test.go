package scheduler_test

import (
	"context"
	"errors"
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
