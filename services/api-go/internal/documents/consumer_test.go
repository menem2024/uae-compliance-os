package documents_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/documents"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

// fakeMsg implements the two jetstream.Msg methods Handle uses; any other call panics.
type fakeMsg struct {
	jetstream.Msg
	subject string
	data    []byte
}

func (m fakeMsg) Subject() string { return m.subject }
func (m fakeMsg) Data() []byte    { return m.data }

func TestConsumerEventsDurableConfig(t *testing.T) {
	cfg := documents.EventsDurableConfig()
	if cfg.Stream != "DOCUMENTS" || cfg.Name != "api-documents" || len(cfg.FilterSubjects) != 2 ||
		cfg.FilterSubjects[0] != events.DocumentExtractedSubject || cfg.FilterSubjects[1] != events.DocumentFailedSubject ||
		cfg.DLQSubject != "dlq.document.results" {
		t.Fatalf("%+v", cfg)
	}
}

func TestConsumerExtractedInsertsAndPublishesAcceptedOnly(t *testing.T) {
	r := newRig()
	store, bus := r.store, r.bus
	c := &documents.Consumer{Store: store, Bus: bus}
	ctx := context.Background()
	docID := uuid.New()
	store.docs[docID] = sqlcDoc(r.firm, docID, "uploaded")
	runID := uuid.NewString()

	accepted := &compliancev1.ExtractedInvoice{SourceOrdinal: 0, SourceRef: "row-0",
		Invoice:    &compliancev1.Invoice{InvoiceNumber: "A-1", TotalAmount: "100.00"},
		Confidence: 0.95, Verdict: &compliancev1.VerifierVerdict{Verdict: compliancev1.Verdict_VERDICT_ACCEPT}}
	revise := &compliancev1.ExtractedInvoice{SourceOrdinal: 1, SourceRef: "row-1",
		Invoice:    &compliancev1.Invoice{InvoiceNumber: "A-2", TotalAmount: "50.00"},
		Confidence: 0.6, Verdict: &compliancev1.VerifierVerdict{Verdict: compliancev1.Verdict_VERDICT_REVISE}}
	m := &compliancev1.DocumentExtracted{DocumentId: docID.String(), FirmId: r.firm.String(), ClientCompanyId: r.cc.String(),
		RunId: runID, DocumentKind: "invoice", Direction: "issued", Language: "en", ExtractionMethod: "llm",
		Invoices: []*compliancev1.ExtractedInvoice{accepted, revise}}
	data, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	doc := store.docs[docID]
	if doc.Status != "extracted" || doc.InvoiceCount != 2 {
		t.Fatalf("%+v", doc)
	}
	rows := store.invoices[docID]
	if len(rows) != 2 || rows[0].out.Status != "extracted" || rows[1].out.Status != "needs_review" {
		t.Fatalf("%+v", rows)
	}
	if len(bus.published) != 1 {
		t.Fatalf("published: %+v", bus.published)
	}
	ev := bus.published[0]
	if ev.subject != events.ExtractedSubject || ev.msgID != events.InvoiceExtractedMsgID(rows[0].out.ID.String()) {
		t.Fatalf("%+v", ev)
	}
	inv, ok := ev.msg.(*compliancev1.InvoiceExtracted)
	if !ok || inv.GetInvoice().GetInvoiceNumber() != "A-1" || inv.GetFirmId() != r.firm.String() {
		t.Fatalf("%+v", ev.msg)
	}

	// Redelivery: Status is no longer uploaded/processing, so ApplyExtracted reports applied=false.
	bus.published = nil
	before := len(store.invoices[docID])
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	if len(store.invoices[docID]) != before || len(bus.published) != 0 {
		t.Fatalf("redelivery must be a no-op: invoices=%d published=%v", len(store.invoices[docID]), bus.published)
	}
}

func TestConsumerExtractedContractSkipsInvoices(t *testing.T) {
	r := newRig()
	store, bus := r.store, r.bus
	c := &documents.Consumer{Store: store, Bus: bus}
	docID := uuid.New()
	store.docs[docID] = sqlcDoc(r.firm, docID, "uploaded")
	m := &compliancev1.DocumentExtracted{DocumentId: docID.String(), FirmId: r.firm.String(), ClientCompanyId: r.cc.String(),
		RunId: uuid.NewString(), DocumentKind: "contract",
		Invoices: []*compliancev1.ExtractedInvoice{{SourceOrdinal: 0, Invoice: &compliancev1.Invoice{InvoiceNumber: "X"}}}}
	data, _ := proto.Marshal(m)
	if err := c.Handle(context.Background(), fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	doc := store.docs[docID]
	if doc.Status != "not_invoice" || doc.InvoiceCount != 0 {
		t.Fatalf("%+v", doc)
	}
	if len(store.invoices[docID]) != 0 || len(bus.published) != 0 {
		t.Fatalf("invoices=%v published=%v", store.invoices[docID], bus.published)
	}
}

func TestConsumerFailedSetsReason(t *testing.T) {
	r := newRig()
	store := r.store
	c := &documents.Consumer{Store: store, Bus: r.bus}
	docID := uuid.New()
	store.docs[docID] = sqlcDoc(r.firm, docID, "processing")
	m := &compliancev1.DocumentFailed{DocumentId: docID.String(), FirmId: r.firm.String(), RunId: uuid.NewString(),
		ReasonCode: "unreadable_pdf", Detail: "pypdfium2: no pages"}
	data, _ := proto.Marshal(m)
	if err := c.Handle(context.Background(), fakeMsg{subject: events.DocumentFailedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	doc := store.docs[docID]
	if doc.Status != "failed" || doc.StatusReason != "unreadable_pdf" {
		t.Fatalf("%+v", doc)
	}
}

func TestConsumerDeadLettersWhatCanNeverSucceed(t *testing.T) {
	c := &documents.Consumer{}
	ctx := context.Background()
	bad, _ := proto.Marshal(&compliancev1.DocumentExtracted{DocumentId: "nope", FirmId: uuid.NewString()})
	for _, m := range []fakeMsg{
		{subject: "document.uploaded", data: nil},
		{subject: events.DocumentExtractedSubject, data: []byte{0xff, 0xff, 0xff}},
		{subject: events.DocumentExtractedSubject, data: bad},
		{subject: events.DocumentFailedSubject, data: nil},
	} {
		if err := c.Handle(ctx, m); !errors.Is(err, events.ErrPermanent) {
			t.Errorf("%s: %v", m.subject, err)
		}
	}
}
