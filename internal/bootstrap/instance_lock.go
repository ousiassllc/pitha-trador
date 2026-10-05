package bootstrap

import (
	"path/filepath"

	"github.com/ousiassllc/pitha-trador/internal/singleinstance"
)

// Lock file names, stored next to the SQLite DB. The app and the
// --supervise watcher take separate locks (the autostart watcher and the
// app it spawns run at once; two watchers or two apps must never
// coexist). cmd/desktop and cmd/server share AppLockName, so a desktop
// and a headless server on the same DB exclude each other too.
const (
	AppLockName        = "app.lock"
	SupervisorLockName = "supervisor.lock"
)

// AcquireInstanceLock takes the named single-instance lock next to the DB
// (resolved like Run does): the DB is the shared resource two instances
// must not both drive. Call it before Run/BuildServices/Services.Start so
// a second process never recovers the first one's running jobs. It
// returns singleinstance.ErrAlreadyRunning when another process holds it.
func AcquireInstanceLock(name string) (*singleinstance.Lock, error) {
	dbPath, err := resolveDBPath("")
	if err != nil {
		return nil, err
	}
	return singleinstance.Acquire(filepath.Join(filepath.Dir(dbPath), name))
}
