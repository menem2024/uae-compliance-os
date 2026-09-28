package db

const (
	StatusUploaded  = "uploaded"
	StatusExtracted = "extracted"
	StatusValidated = "validated"
	StatusHasIssues = "has_issues"
)

// IsTerminal reports whether status is a final validation outcome. An
// invoice.extracted event for an invoice already in a terminal status is a
// safe-to-skip redelivery (e.g. after the api-validation durable is
// recreated and the retained INVOICES stream replays).
func IsTerminal(status string) bool {
	return status == StatusValidated || status == StatusHasIssues
}
