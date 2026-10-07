// Package proposals serves GET /api/v1/policy-proposals, backed by the
// PolicyProposalSource port (internal/service/assist).
// It depends only on internal/service and internal/domain; it MUST NOT import
// sibling handler subpackages.
package proposals
