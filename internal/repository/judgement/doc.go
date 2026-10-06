// Package judgement persists the Jev judgement data and its evaluation:
// jev_decisions (DecisionRepository, decision_repo.go) and policy_proposals
// (ProposalRepository, proposal_repo.go). Its calibration_outcomes
// evaluation data lives in internal/repository/calibration.
//
// It MUST depend only on internal/domain and internal/repository/sqlutil.
// See docs/architecture/overview.md §3.
package judgement
