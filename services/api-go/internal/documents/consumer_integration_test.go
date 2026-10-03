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

	// Redelivery of the same run: no new rows; invoice.extracted is published again (same Msg-Id, so
	// the stream dedups it) in case the first publish was lost.
	first := bus.published[0]
	bus.published = nil
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	if len(bus.published) != 1 || bus.published[0].msgID != first.msgID {
		t.Fatalf("redelivery must re-publish with the same Msg-Id: %+v vs %+v", bus.published, first)
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
	if _, err := store.ApplyExtractedResult(ctx, env.FirmA, documents.ExtractedParams{ID: docID, Status: "extracted", Kind: "invoice"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reprocess(ctx, env.FirmA, docID); !errors.Is(err, documents.ErrNotReprocessable) {
		t.Fatalf("%v", err)
	}
}

// extractedMsg builds a document.extracted result with one accepted invoice per ordinal.
func extractedMsg(env dbtest.Env, docID, cc uuid.UUID, runID string, ordinals ...int32) []byte {
	var invs []*compliancev1.ExtractedInvoice
	for _, o := range ordinals {
		invs = append(invs, &compliancev1.ExtractedInvoice{SourceOrdinal: o, SourceRef: "row",
			Invoice:    &compliancev1.Invoice{InvoiceNumber: "A-1", TotalAmount: "100.00"},
			Confidence: 0.95, Verdict: &compliancev1.VerifierVerdict{Verdict: compliancev1.Verdict_VERDICT_ACCEPT}})
	}
	data, _ := proto.Marshal(&compliancev1.DocumentExtracted{DocumentId: docID.String(), FirmId: env.FirmA.String(),
		ClientCompanyId: cc.String(), RunId: runID, DocumentKind: "invoice", Direction: "issued", Language: "en",
		ExtractionMethod: "llm", Invoices: invs})
	return data
}

// P2 (security review A, finding 1): a failed invoice.extracted publish must not strand the Document
// as 'extracted'. The first delivery errors (nak), the redelivery re-publishes.
func TestConsumerIntegrationLostPublishSelfHeals(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	store := documents.PGStore{Pool: env.App}
	bus := &fakeBus{err: errors.New("nats: timeout")}
	c := &documents.Consumer{Store: store, Bus: bus}
	cc := dbtest.ClientCompany(t, env, env.FirmA, "Oasis", "")
	docID := uploadedDocument(t, store, env, env.FirmA, cc)
	data := extractedMsg(env, docID, cc, uuid.NewString(), 0)

	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err == nil ||
		errors.Is(err, events.ErrPermanent) {
		t.Fatalf("a failed publish must be retried (nak), got %v", err)
	}
	bus.err = nil
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	if len(bus.published) != 1 || bus.published[0].subject != events.ExtractedSubject {
		t.Fatalf("redelivery must re-publish invoice.extracted: %+v", bus.published)
	}
	inv, err := store.Invoices(ctx, env.FirmA, docID)
	if err != nil || len(inv) != 1 || bus.published[0].msgID != events.InvoiceExtractedMsgID(inv[0].ID.String()) {
		t.Fatalf("%+v %v %+v", inv, err, bus.published)
	}
}

// P2 (finding 1): a result that cannot be applied in full must leave the Document untouched, not
// committed as 'extracted' with its invoices missing.
func TestConsumerIntegrationBadResultRollsBackTheDocument(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	store := documents.PGStore{Pool: env.App}
	bus := &fakeBus{}
	c := &documents.Consumer{Store: store, Bus: bus}
	cc := dbtest.ClientCompany(t, env, env.FirmA, "Oasis", "")

	for name, data := range map[string]func(docID uuid.UUID) []byte{
		"negative ordinal (CHECK 23514)": func(id uuid.UUID) []byte { return extractedMsg(env, id, cc, uuid.NewString(), 0, -1) },
		"unparsable client_company_id": func(id uuid.UUID) []byte {
			m := &compliancev1.DocumentExtracted{}
			_ = proto.Unmarshal(extractedMsg(env, id, cc, uuid.NewString(), 0), m)
			m.ClientCompanyId = "not-a-uuid"
			b, _ := proto.Marshal(m)
			return b
		},
	} {
		docID := uploadedDocument(t, store, env, env.FirmA, cc)
		err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data(docID)})
		if !errors.Is(err, events.ErrPermanent) {
			t.Fatalf("%s: want a permanent error, got %v", name, err)
		}
		doc, gerr := store.Get(ctx, env.FirmA, docID)
		if gerr != nil || doc.Status != "uploaded" || doc.InvoiceCount != 0 {
			t.Fatalf("%s: the Document must not be committed as extracted: %+v %v", name, doc, gerr)
		}
		if inv, _ := store.Invoices(ctx, env.FirmA, docID); len(inv) != 0 {
			t.Fatalf("%s: %+v", name, inv)
		}
	}
	if len(bus.published) != 0 {
		t.Fatalf("%+v", bus.published)
	}
}

// P2 (finding 2): the invoices take the ClientCompany and direction of the locked Document row, so a
// document.attribution accepted before document.extracted arrives is not undone by the late result.
func TestConsumerIntegrationInvoicesFollowTheLockedDocument(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	store := documents.PGStore{Pool: env.App}
	c := &documents.Consumer{Store: store, Bus: &fakeBus{}}
	oldCC := dbtest.ClientCompany(t, env, env.FirmA, "Old", "")
	newCC := dbtest.ClientCompany(t, env, env.FirmA, "New", "")
	docID := uploadedDocument(t, store, env, env.FirmA, oldCC)
	if _, err := dbtest.ExecFirm(ctx, env, env.FirmA,
		`UPDATE documents SET client_company_id = $1, direction = 'received' WHERE id = $2`, newCC, docID); err != nil {
		t.Fatal(err)
	}

	data := extractedMsg(env, docID, oldCC, uuid.NewString(), 0) // the message still names the OLD company
	if err := c.Handle(ctx, fakeMsg{subject: events.DocumentExtractedSubject, data: data}); err != nil {
		t.Fatal(err)
	}
	var invCC, docCC uuid.UUID
	var dir string
	if err := dbtest.QueryRowFirm(ctx, env, env.FirmA,
		`SELECT client_company_id FROM invoices WHERE document_id = $1`, docID).Scan(&invCC); err != nil {
		t.Fatal(err)
	}
	if err := dbtest.QueryRowFirm(ctx, env, env.FirmA,
		`SELECT client_company_id, direction FROM documents WHERE id = $1`, docID).Scan(&docCC, &dir); err != nil {
		t.Fatal(err)
	}
	if invCC != newCC || docCC != newCC || dir != "received" {
		t.Fatalf("invoice cc=%s doc cc=%s (want %s) direction=%q (want received)", invCC, docCC, newCC, dir)
	}
}
