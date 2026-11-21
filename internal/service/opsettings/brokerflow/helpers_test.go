// Package brokerflow_test checks the broker settings (private key paths,
// broker.* scalars, LoadBroker) saved through opsettings.Service. It lives in
// its own directory to keep internal/service/opsettings within the linterly
// directory budget.
package brokerflow_test

import (
	"context"
	"time"

	"github.com/ousiassllc/pitha-trador/internal/config"
	"github.com/ousiassllc/pitha-trador/internal/service/opsettings"
)

type fakeStore struct {
	rows   map[string]string
	getErr error
}

func (f *fakeStore) Get(_ context.Context, key string) (string, bool, error) {
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.rows[key]
	return v, ok, nil
}

func (f *fakeStore) Set(_ context.Context, key, value string, _ time.Time) error {
	f.rows[key] = value
	return nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	delete(f.rows, key)
	return nil
}

func newService(store *fakeStore) *opsettings.Service {
	return opsettings.New(store, config.StrategyConfig{}, "/data/logs")
}
