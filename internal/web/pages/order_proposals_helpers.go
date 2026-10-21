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
	return "py-1 " + pnlTextClass(sign)
}

// pnlTextClass colours a gain green and a loss red; anything else is plain.
func pnlTextClass(sign string) string {
	switch sign {
	case "gain":
		return "text-emerald-700"
	case "loss":
		return "text-red-700"
	}
	return ""
}

// ProposalStatusLabel is the operator-facing name of a status code (the code
// itself when it is unknown), for the list's status filter.
func ProposalStatusLabel(code string) string { return label(proposalStatusLabels, code) }
