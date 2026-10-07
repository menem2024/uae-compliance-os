package events

import "time"

// Track B streams, subjects and message ids (agent-runtime contract v0.2, section 4).
const (
	DocumentsStream = "DOCUMENTS"
	AgentsStream    = "AGENTS"

	DocumentUploadedSubject     = "document.uploaded"
	DocumentExtractedSubject    = "document.extracted"
	DocumentFailedSubject       = "document.failed"
	AgentRunStartedSubject      = "agent.run.started"
	AgentRunStepSubject         = "agent.run.step"
	AgentRunFinishedSubject     = "agent.run.finished"
	AgentProposalCreatedSubject = "agent.proposal.created"
	AgentTaskRequestedSubject   = "agent.task.requested"

	DLQDocumentResultsSubject = "dlq.document.results"
	DLQAgentEventsSubject     = "dlq.agent.events"

	// DocumentsDurable consumes document.extracted and document.failed.
	DocumentsDurable = "api-documents"
	// AgentEventsDurable consumes agent.run.> and agent.proposal.>.
	AgentEventsDurable = "api-agent-events"

	documentsMaxAge = 30 * 24 * time.Hour
	agentsMaxAge    = 7 * 24 * time.Hour
	duplicateWindow = 2 * time.Minute
)

// DocumentUploadedMsgID is the Nats-Msg-Id of document.uploaded: a reprocess
// rotates the nonce, so it is a new message.
func DocumentUploadedMsgID(documentID, reprocessNonce string) string {
	return DocumentUploadedSubject + ":" + documentID + ":" + reprocessNonce
}

// InvoiceExtractedMsgID is the Nats-Msg-Id api-go uses when it publishes
// invoice.extracted for an invoice created from a document.
func InvoiceExtractedMsgID(invoiceID string) string {
	return ExtractedSubject + ":" + invoiceID
}
