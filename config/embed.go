// Package configdefaults embeds config/strategy.yaml and config/risk.yaml
// so a packaged .exe still has a usable default configuration even when
// distributed with no accompanying config/ directory next to it
// (docs/architecture/overview.md §7, issue #59).
//
// internal/bootstrap.Run resolves each config file's path in four steps:
// an explicit bootstrap.Config field, then the PITHA_STRATEGY_PATH/
// PITHA_RISK_PATH environment variable, then a config/ directory next to
// the running executable (os.Executable), and only once all three are
// absent does it fall back to parsing these embedded bytes via
// internal/config's LoadStrategyBytes/LoadRiskBytes.
//
// The go:embed directive can only reference paths under this file's own directory
// (no `..`), which is why this package lives here rather than inside
// internal/config: it is a thin embed of the two YAML files that package
// already knows how to parse.
package configdefaults

import _ "embed"

//go:embed strategy.yaml
var DefaultStrategyYAML []byte

//go:embed risk.yaml
var DefaultRiskYAML []byte
