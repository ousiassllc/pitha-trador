package paperresultsmcp

import "github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchquotediag"

// research_quote_source_compare: kabu vs the 立花 research 時価 samples (#1083, API §3.36).
func init() {
	tools = append(tools, queryTool("research_quote_source_compare", researchquotediag.Description, researchquotediag.SchemaProperties(), researchquotediag.Run))
}
