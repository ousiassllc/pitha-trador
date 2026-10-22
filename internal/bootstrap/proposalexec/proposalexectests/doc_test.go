// Package proposalexectests holds the tests of package proposalexec (issues
// #828, #857, #859): the order-proposal flow on the real Paper wiring (Risk
// Engine, Execution Engine, the migrated schema). They live in a sibling
// directory, apart from the package, so that neither exceeds linterly's
// per-directory line limit (the same arrangement as proposaltests and
// mcptests). They use only the exported names of proposalexec.
package proposalexectests
