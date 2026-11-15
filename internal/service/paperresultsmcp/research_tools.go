package paperresultsmcp

import (
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/exportdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/ledgerdiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/promotiondiag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/slicediag"
	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/trenddiag"
)

func init() { tools = append(tools, researchTools...) }

// The research tools of issues #1089 (slices, trend), #1090 (candidate_ledger), #1091 (promotion_checklist)
// and #1094 (training_dataset_export). All read only the database; training_dataset_export alone writes,
// and only files in the local exports folder, so it is the one not marked read-only.
var researchTools = []tool{
	queryTool("jev_evaluation_slices", slicediag.Description, slicediag.SchemaProperties(), slicediag.Run),
	queryTool("jev_evaluation_trend", trenddiag.Description, trenddiag.SchemaProperties(), trenddiag.Run),
	queryTool("candidate_ledger", ledgerdiag.Description, ledgerdiag.SchemaProperties(), ledgerdiag.Run),
	queryTool("promotion_checklist", promotiondiag.Description, promotiondiag.SchemaProperties(), promotiondiag.Run),
	func() tool {
		t := queryTool("training_dataset_export", exportdiag.Description, exportdiag.SchemaProperties(), exportdiag.Run)
		t.writes = true
		return t
	}(),
}
