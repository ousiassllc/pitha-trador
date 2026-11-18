package researchhub_test

import (
	"github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/adapter"
	tt "github.com/ousiassllc/pitha-trador/internal/service/broker/tachibana/tachibanatest"
)

// adapterBase trusts the fake broker's TLS certificate.
func adapterBase(fb *tt.FakeBroker) adapter.Config {
	return adapter.Config{HTTPClient: fb.Server().Client()}
}
