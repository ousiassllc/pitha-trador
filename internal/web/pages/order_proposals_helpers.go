package pages

// dash shows "—" for a value the proposal does not have (yet).
func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// pnlClass is the classes of a realized pnl table cell.
func pnlClass(sign string) string {
	return "py-1 text-right font-mono tabular-nums " + pnlTextClass(sign)
}

// pnlTextClass colours a gain green and a loss red; anything else is plain.
func pnlTextClass(sign string) string {
	switch sign {
	case "gain":
		return "text-success"
	case "loss":
		return "text-danger"
	}
	return ""
}

// ProposalStatusLabel is the operator-facing name of a status code (the code
// itself when it is unknown), for the list's status filter.
func ProposalStatusLabel(code string) string { return label(proposalStatusLabels, code) }
