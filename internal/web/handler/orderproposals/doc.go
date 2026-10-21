// Package orderproposals serves the 注文案 screens (issue #829, child of
// #826): `GET /order-proposals` (the list of the Paper order proposals the MCP
// client made) and `GET /order-proposals/:id` (one proposal with its plan,
// sources, timeline and the linked signal, order and position).
// They only read, through the Source port, which is the read model the Paper
// results MCP's order_proposals_list / order_proposal_get use
// (internal/service/paperresultsmcp/proposal); there is no action route.
// The package depends only on internal/service, internal/domain and
// handler/shared; it MUST NOT import sibling handler subpackages.
package orderproposals
