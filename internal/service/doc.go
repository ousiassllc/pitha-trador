// Package service is the parent package for the application's business
// logic sub-packages (marketdata, featureengine, screener, jev, rag, policy,
// risk, execution, calibration, assist, selfimprove, scheduler, ...).
//
// Sub-packages under internal/service MUST depend only on internal/domain
// and internal/repository. External I/O (HTTP clients for kabu station,
// Jev/Luna/Sol/Opus APIs, ...) belongs in this layer. See
// docs/architecture/overview.md §3/§4 for the layer dependency rules and
// component responsibilities.
//
// Concrete sub-packages are introduced by later sub-scopes; this file only
// establishes the package skeleton.
package service
