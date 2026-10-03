package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// EventsDurableConfig is the api-documents durable (contract section 4): it consumes
// document.extracted and document.failed, the only results ai-py ever publishes for a Document.
func EventsDurableConfig() events.DurableConfig {
	return events.DurableConfig{
		Stream:         events.DocumentsStream,
		Name:           events.DocumentsDurable,
		FilterSubjects: []string{events.DocumentExtractedSubject, events.DocumentFailedSubject},
		DLQSubject:     events.DLQDocumentResultsSubject,
		Workers:        4,
	}
}

// Consumer turns document.extracted/document.failed into invoice rows, invoice.extracted and the
// Document's terminal status. Every write is gated by Store.ApplyExtracted/ApplyFailed's affected-row
// count, so a redelivery or a superseded run's result is a safe no-op (Task 17 design decisions).
type Consumer struct {
	Store Store
	Bus   events.ProtoPublisher
}

func permanent(format string, args ...any) error {
	return fmt.Errorf("%w: %s", events.ErrPermanent, fmt.Sprintf(format, args...))
}

func unmarshalProto(data []byte, m proto.Message) error {
	if err := proto.Unmarshal(data, m); err != nil {
		return permanent("decode %T: %v", m, err)
	}
	return nil
}

// Handle is the events.MsgHandler for the api-documents durable.
func (c *Consumer) Handle(ctx context.Context, msg jetstream.Msg) error {
	var err error
	switch msg.Subject() {
	case events.DocumentExtractedSubject:
		m := &compliancev1.DocumentExtracted{}
		if err = unmarshalProto(msg.Data(), m); err == nil {
			err = c.extracted(ctx, m)
		}
	case events.DocumentFailedSubject:
		m := &compliancev1.DocumentFailed{}
		if err = unmarshalProto(msg.Data(), m); err == nil {
			err = c.failed(ctx, m)
		}
	default:
		return permanent("unexpected subject %s", msg.Subject())
	}
	if err != nil && !errors.Is(err, events.ErrPermanent) && proposals.IsPermanentPG(err) {
		return fmt.Errorf("%w: %w", events.ErrPermanent, err)
	}
	return err
}

func (c *Consumer) extracted(ctx context.Context, m *compliancev1.DocumentExtracted) error {
	id, err := uuid.Parse(m.GetDocumentId())
	if err != nil {
		return permanent("document_id %q", m.GetDocumentId())
	}
	firm, err := uuid.Parse(m.GetFirmId())
	if err != nil {
		return permanent("firm_id %q", m.GetFirmId())
	}
	runID, err := uuid.Parse(m.GetRunId())
	if err != nil {
		return permanent("run_id %q", m.GetRunId())
	}
	status := DocumentStatus(m.GetDocumentKind(), m.GetNeedsReview())
	reviewReasons := m.GetReviewReasons()
	if reviewReasons == nil {
		reviewReasons = []string{}
	}
	invoiceCount := int32(0)
	if status != "not_invoice" {
		invoiceCount = int32(len(m.GetInvoices())) //nolint:gosec // bounded by the upload/extraction path
	}
	applied, err := c.Store.ApplyExtracted(ctx, firm, ExtractedParams{
		ID: id, RunID: runID, Status: status, Kind: m.GetDocumentKind(), Direction: m.GetDirection(),
		Language: m.GetLanguage(), ExtractionMethod: m.GetExtractionMethod(), ReviewReasons: reviewReasons,
		InvoiceCount: invoiceCount})
	if err != nil {
		return fmt.Errorf("apply extracted: %w", err)
	}
	if !applied || status == "not_invoice" {
		return nil
	}
	cc, err := uuid.Parse(m.GetClientCompanyId())
	if err != nil {
		return permanent("client_company_id %q", m.GetClientCompanyId())
	}
	items := make([]InvoiceIn, 0, len(m.GetInvoices()))
	for _, ei := range m.GetInvoices() {
		payload, err := protojson.Marshal(ei.GetInvoice())
		if err != nil {
			return permanent("marshal invoice: %v", err)
		}
		items = append(items, InvoiceIn{SourceOrdinal: ei.GetSourceOrdinal(), SourceRef: ei.GetSourceRef(),
			Payload: payload, Status: InvoiceStatus(ei.GetVerdict().GetVerdict(), ei.GetConfidence()),
			Confidence: ei.GetConfidence(), ClientCompanyID: cc})
	}
	out, err := c.Store.InsertInvoices(ctx, firm, id, items)
	if err != nil {
		return fmt.Errorf("insert invoices: %w", err)
	}
	for _, o := range out {
		if o.Status != "extracted" {
			continue
		}
		inv := &compliancev1.Invoice{}
		if err := protojson.Unmarshal(o.Payload, inv); err != nil {
			return fmt.Errorf("decode stored invoice %s: %w", o.ID, err)
		}
		confidence := 0.0
		if o.Confidence != nil {
			confidence = *o.Confidence
		}
		ev := &compliancev1.InvoiceExtracted{InvoiceId: o.ID.String(), FirmId: firm.String(), Invoice: inv, Confidence: confidence}
		if err := c.Bus.Publish(ctx, events.ExtractedSubject, events.InvoiceExtractedMsgID(o.ID.String()), ev); err != nil {
			return fmt.Errorf("publish invoice.extracted %s: %w", o.ID, err)
		}
	}
	return nil
}

func (c *Consumer) failed(ctx context.Context, m *compliancev1.DocumentFailed) error {
	id, err := uuid.Parse(m.GetDocumentId())
	if err != nil {
		return permanent("document_id %q", m.GetDocumentId())
	}
	firm, err := uuid.Parse(m.GetFirmId())
	if err != nil {
		return permanent("firm_id %q", m.GetFirmId())
	}
	var runID uuid.UUID
	if rid := m.GetRunId(); rid != "" {
		if runID, err = uuid.Parse(rid); err != nil {
			return permanent("run_id %q", rid)
		}
	}
	if _, err := c.Store.ApplyFailed(ctx, firm, id, runID, m.GetReasonCode()); err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}
	return nil
}
