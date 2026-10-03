//go:build integration

package documents_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/documents"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

// uploadedDocument prepares and finishes an upload against the real store, returning a Document
// that is in "uploaded" status and ready for a document.extracted/document.failed result.
func uploadedDocument(t *testing.T, store documents.PGStore, env dbtest.Env, firm, cc uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	f := documents.FileIn{Filename: "inv.pdf", ContentType: "application/pdf", SizeBytes: 29, SHA256: uuid.New().String() + uuid.New().String()}
	// SHA256 must look like 64 lowercase hex; build one deterministically from two UUIDs' hex digits.
	f.SHA256 = hexOf(f.SHA256)
	p, err := store.PreparePending(ctx, firm, cc, "user-1", []documents.FileIn{f})
	if err != nil || len(p) != 1 {
		t.Fatalf("prepare pending: %+v %v", p, err)
	}
	doc, moved, err := store.FinishUpload(ctx, firm, p[0].Doc.ID, "")
	if err != nil || !moved || doc.Status != "uploaded" {
		t.Fatalf("finish upload: %+v %v %v", doc, moved, err)
	}
	return doc.ID
}

// hexOf keeps only hex-safe characters from s and pads/truncates to exactly 64 lowercase hex chars,
// giving each test a unique, valid-looking sha256 without computing a real digest.
func hexOf(s string) string {
	out := make([]byte, 0, 64)
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
			out = append(out, byte(r))
		case r >= 'A' && r <= 'F':
			out = append(out, byte(r-'A'+'a'))
		}
		if len(out) == 64 {
			break
		}
	}
	for len(out) < 64 {
		out = append(out, '0')
	}
	return string(out)
}

func TestConsumerIntegrationRoundTrip(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	store := documents.PGStore{Pool: env.App}
	bus := &fakeBus{}
	c := &documents.Consumer{Store: store, Bus: bus}

	cc := dbtest.ClientCompany(t, env, env.FirmA, "Oasis", "")
	docID := uploadedDocument(t, store, env, env.FirmA, cc)

	accepted := &compliancev1.ExtractedInvoice{SourceOrdinal: 0, SourceRef: "row-0",
		Invoice:    &compliancev1.Invoice{InvoiceNumber: "A-1", TotalAmount: "100.00"},
		Confidence: 0.95, Verdict: &compliancev1.VerifierVerdict{Verdict: compliancev1.Verdict_VERDICT_ACCEPT}}
	revise := &compliancev1.ExtractedInvoice{SourceOrdinal: 1, SourceRef: "row-1",
		Invoice:    &compliancev1.Invoice{InvoiceNumber: "A-2", TotalAmount: "50.00"},
		Confidence: 0.6, Verdict: &compliancev1.VerifierVerdict{Verdict: compliancev1.Verdict_VERDICT_REVISE}}
	m := &compliancev1.DocumentExtracted{DocumentId: docID.String(), FirmId: env.FirmA.String(), ClientCompanyId: cc.String(),
		RunId: uuid.NewString(), DocumentKind: "invoice", Direction: "issued", Language: "en", ExtractionMethod: "llm",
		Invoices: []*compliancev1.ExtractedInvoice{accepted, revise}}
	data, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}

	doc, err := store.Get(ctx, env.FirmA, docID)
	if err != nil || doc.Status != "extracted" || doc.InvoiceCount != 2 {
		t.Fatalf("%+v %v", doc, err)
	}
	inv, err := store.Invoices(ctx, env.FirmA, docID)
	if err != nil || len(inv) != 2 {
		t.Fatalf("%+v %v", inv, err)
	}
	byOrdinal := map[int32]documents.InvoiceRef{}
	for _, i := range inv {
		byOrdinal[i.SourceOrdinal] = i
	}
	if byOrdinal[0].Status != "extracted" || byOrdinal[1].Status != "needs_review" {
		t.Fatalf("%+v", byOrdinal)
	}
	if len(bus.published) != 1 || bus.published[0].subject != events.ExtractedSubject {
		t.Fatalf("published: %+v", bus.published)
	}

	// Redelivery: the row is no longer uploaded/processing, so the consumer must do nothing else.
	bus.published = nil
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	if len(bus.published) != 0 {
		t.Fatalf("redelivery must be a no-op: %+v", bus.published)
	}
	inv2, err := store.Invoices(ctx, env.FirmA, docID)
	if err != nil || len(inv2) != 2 {
		t.Fatalf("redelivery must not double-insert: %+v %v", inv2, err)
	}
}

func TestConsumerIntegrationCrossFirmIsolation(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	store := documents.PGStore{Pool: env.App}
	c := &documents.Consumer{Store: store, Bus: &fakeBus{}}

	cc := dbtest.ClientCompany(t, env, env.FirmA, "Alpha", "")
	docID := uploadedDocument(t, store, env, env.FirmA, cc)

	m := &compliancev1.DocumentExtracted{DocumentId: docID.String(), FirmId: env.FirmA.String(), ClientCompanyId: cc.String(),
		RunId: uuid.NewString(), DocumentKind: "invoice",
		Invoices: []*compliancev1.ExtractedInvoice{{SourceOrdinal: 0, SourceRef: "r0",
			Invoice: &compliancev1.Invoice{InvoiceNumber: "X-1"}, Confidence: 0.99,
			Verdict: &compliancev1.VerifierVerdict{Verdict: compliancev1.Verdict_VERDICT_ACCEPT}}}}
	data, _ := proto.Marshal(m)
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.Get(ctx, env.FirmB, docID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("cross-firm get: %v", err)
	}
	if rows, err := store.List(ctx, env.FirmB, documents.ListFilter{Limit: 10}); err != nil || len(rows) != 0 {
		t.Fatalf("cross-firm list leaked: %d %v", len(rows), err)
	}
}

func TestServiceReprocessIntegration(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	store := documents.PGStore{Pool: env.App}
	bus := &fakeBus{}
	svc := &documents.Service{Store: store, Bus: bus}

	cc := dbtest.ClientCompany(t, env, env.FirmA, "Beta", "")
	docID := uploadedDocument(t, store, env, env.FirmA, cc)
	if _, err := store.ApplyFailed(ctx, env.FirmA, docID, uuid.Nil, "unreadable_pdf"); err != nil {
		t.Fatal(err)
	}
	before, err := store.Get(ctx, env.FirmA, docID)
	if err != nil || before.Status != "failed" {
		t.Fatalf("%+v %v", before, err)
	}

	status, err := svc.Reprocess(ctx, env.FirmA, docID)
	if err != nil || status != "uploaded" {
		t.Fatalf("%v %v", status, err)
	}
	after, err := store.Get(ctx, env.FirmA, docID)
	if err != nil || after.Status != "uploaded" || after.ReprocessNonce == before.ReprocessNonce {
		t.Fatalf("%+v %v", after, err)
	}
	if len(bus.published) != 1 {
		t.Fatalf("reprocess must republish document.uploaded: %+v", bus.published)
	}
	if _, ok := bus.published[0].msg.(*compliancev1.DocumentUploaded); !ok {
		t.Fatalf("%+v", bus.published[0].msg)
	}

	// Move the Document to a terminal, non-reprocessable state and verify the error.
	if _, err := store.ApplyExtracted(ctx, env.FirmA, documents.ExtractedParams{ID: docID, Status: "extracted", Kind: "invoice"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reprocess(ctx, env.FirmA, docID); !errors.Is(err, documents.ErrNotReprocessable) {
		t.Fatalf("%v", err)
	}
}
