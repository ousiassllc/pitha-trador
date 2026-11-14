// Package isolationflow holds structural guards that keep the Jev
// model-comparison trial (issue #1059) apart from trading, RAG, Calibration,
// the meta-model and training. It has no production code: the tests scan the
// repository sources and import graph, in both directions (the trial never
// reaches trading; the live path never reaches the trial, issue #1069). The
// shadow's counterpart lives in
// internal/service/jev/evalflow/shadow_isolation_test.go; this directory is
// separate only because that one is at its line budget.
package isolationflow
