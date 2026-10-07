// Package exports creates, lists and serves the PINT-AE XML exports of approved invoices (spec
// §5.6.5). validator-rs renders the document; this package decides whether an export may happen,
// stores the bytes, and records who exported what.
package exports

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/minio/minio-go/v7"
	"google.golang.org/protobuf/encoding/protojson"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// MaxObjectBytes bounds one export document (the same 16 MiB as the gRPC message limit).
const MaxObjectBytes = 16 << 20

// ContentType is stored with the object and served on download.
const ContentType = "application/xml; charset=utf-8"

var (
	// ErrNotReady: the invoice is not 'ready', its approval or latest run does not match its current
	// payload, or the latest run has errors.
	ErrNotReady = errors.New("exports: invoice not ready for export")
	// ErrRevalidationRequired: validator-rs found errors when it validated the stored payload for
	// the export, so no document was produced.
	ErrRevalidationRequired = errors.New("exports: revalidation required")
	// ErrCorrupt: the stored object does not match the recorded size or sha256.
	ErrCorrupt = errors.New("exports: stored object does not match its recorded digest")
)

// Exporter is the validator-rs ExportService client (compliancev1connect.ExportServiceClient
// satisfies it).
type Exporter interface {
	Export(context.Context, *connect.Request[compliancev1.ExportRequest]) (*connect.Response[compliancev1.ExportResponse], error)
}

// ObjectStore keeps the export documents.
type ObjectStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
}

// MinioStore is an ObjectStore on one MinIO/S3 bucket (storage.DocumentsBucket in production).
type MinioStore struct {
	Client *minio.Client
	Bucket string
}

// Put stores data under key.
func (m *MinioStore) Put(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := m.Client.PutObject(ctx, m.Bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("exports: put %s: %w", key, err)
	}
	return nil
}

// Get loads the object under key; an object above MaxObjectBytes is an error.
func (m *MinioStore) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := m.Client.GetObject(ctx, m.Bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("exports: get %s: %w", key, err)
	}
	defer func() { _ = obj.Close() }()
	b, err := io.ReadAll(io.LimitReader(obj, MaxObjectBytes+1))
	if err != nil {
		return nil, fmt.Errorf("exports: read %s: %w", key, err)
	}
	if len(b) > MaxObjectBytes {
		return nil, fmt.Errorf("exports: object %s is larger than %d bytes", key, MaxObjectBytes)
	}
	return b, nil
}

// View is an export's metadata (the bytes come from Read).
type View struct {
	ID, InvoiceID, RunID uuid.UUID
	PayloadVersion       int32
	RulesetVersion       string
	Format               string
	DocumentKind         string
	SHA256               string
	SizeBytes            int32
	CreatedBy            string
	CreatedAt            time.Time
	Filename             string // the download name, [A-Za-z0-9._-] only
}

// Service creates and reads exports.
type Service struct {
	Pool     *pgxpool.Pool
	Exporter Exporter
	Store    ObjectStore
	NewID    func() uuid.UUID // uuid.New when nil
}

func (s *Service) newID() uuid.UUID {
	if s.NewID != nil {
		return s.NewID()
	}
	return uuid.New()
}

func hexSHA256(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func objectKey(firmID, exportID uuid.UUID) string {
	return fmt.Sprintf("firms/%s/exports/%s.xml", firmID, exportID)
}

// ready reports whether the invoice may be exported from its latest run.
func ready(inv sqlc.TrackCGetInvoiceRow, run sqlc.TrackCGetRunRow) bool {
	return inv.Status == "ready" &&
		inv.ApprovedPayloadVersion.Valid && inv.ApprovedPayloadVersion.Int32 == inv.PayloadVersion &&
		inv.LatestRunID != uuid.Nil && run.ID == inv.LatestRunID &&
		run.PayloadVersion == inv.PayloadVersion && run.ErrorCount == 0
}

// Create exports the invoice's approved payload: validator-rs re-validates and renders it, the
// document is stored, and one exports row plus the audit event invoice.exported are written in one
// transaction. It returns ErrNotReady, ErrRevalidationRequired, db.ErrNotFound or another error.
func (s *Service) Create(ctx context.Context, firmID, invoiceID uuid.UUID, actor string) (View, error) {
	var (
		inv sqlc.TrackCGetInvoiceRow
		run sqlc.TrackCGetRunRow
	)
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		var err error
		if inv, err = q.TrackCGetInvoice(ctx, invoiceID); err != nil {
			return err
		}
		if inv.LatestRunID == uuid.Nil {
			return ErrNotReady
		}
		if run, err = q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: inv.LatestRunID, InvoiceID: invoiceID}); err != nil {
			return err
		}
		if !ready(inv, run) {
			return ErrNotReady
		}
		return nil
	})
	if err != nil {
		return View{}, err
	}

	var invoice compliancev1.Invoice
	if err := protojson.Unmarshal(inv.Payload, &invoice); err != nil {
		return View{}, fmt.Errorf("exports: stored payload of invoice %s: %w", invoiceID, err)
	}
	resp, err := s.Exporter.Export(ctx, connect.NewRequest(&compliancev1.ExportRequest{
		Invoice: &invoice, RulesetVersion: run.RulesetVersion,
	}))
	if err != nil {
		return View{}, fmt.Errorf("exports: export invoice %s: %w", invoiceID, err)
	}
	m := resp.Msg
	if !m.GetExported() {
		return View{}, ErrRevalidationRequired
	}
	xml := m.GetXml()
	switch {
	case len(xml) == 0 || len(xml) > MaxObjectBytes:
		return View{}, fmt.Errorf("exports: exporter returned %d bytes", len(xml))
	case hexSHA256(xml) != m.GetSha256():
		return View{}, fmt.Errorf("exports: exporter sha256 %q does not match the document (%s)", m.GetSha256(), hexSHA256(xml))
	case m.GetDocumentKind() != "invoice" && m.GetDocumentKind() != "credit_note":
		return View{}, fmt.Errorf("exports: exporter returned document kind %q", m.GetDocumentKind())
	case m.GetFormat() == "":
		return View{}, errors.New("exports: exporter returned no format")
	}

	// The key is random and referenced by nothing until the row below commits, so an object whose
	// transaction fails is an orphan: wasted bytes, never a wrong or visible export.
	id := s.newID()
	key := objectKey(firmID, id)
	if err := s.Store.Put(ctx, key, xml, ContentType); err != nil {
		return View{}, err
	}

	var createdAt time.Time
	err = db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		cur, err := q.TrackCLockInvoice(ctx, invoiceID)
		if err != nil {
			return err
		}
		curInv := sqlc.TrackCGetInvoiceRow(cur)
		if curInv.PayloadVersion != inv.PayloadVersion || !ready(curInv, run) {
			return ErrNotReady // edited, re-approved against another run, or revoked meanwhile
		}
		row, err := q.TrackCInsertExport(ctx, sqlc.TrackCInsertExportParams{
			ID: id, FirmID: firmID, InvoiceID: invoiceID, RunID: run.ID, PayloadVersion: inv.PayloadVersion,
			RulesetVersion: run.RulesetVersion, Format: m.GetFormat(), DocumentKind: m.GetDocumentKind(),
			ObjectKey: key, Sha256: m.GetSha256(), SizeBytes: int32(len(xml)), CreatedBy: actor,
		})
		if err != nil {
			return fmt.Errorf("exports: insert: %w", err)
		}
		createdAt = row.Time
		_, err = audit.Record(ctx, q, firmID, audit.Event{
			Actor:      audit.Actor{Type: "user", ID: actor},
			Action:     "invoice.exported",
			EntityType: "export",
			EntityID:   id,
			InvoiceID:  &invoiceID,
			After: map[string]any{
				"export_id": id.String(), "payload_version": inv.PayloadVersion, "run_id": run.ID.String(),
				"ruleset_version": run.RulesetVersion, "format": m.GetFormat(), "document_kind": m.GetDocumentKind(),
				"sha256": m.GetSha256(), "size_bytes": len(xml),
			},
		})
		return err
	})
	if err != nil {
		return View{}, err
	}
	return View{
		ID: id, InvoiceID: invoiceID, RunID: run.ID, PayloadVersion: inv.PayloadVersion,
		RulesetVersion: run.RulesetVersion, Format: m.GetFormat(), DocumentKind: m.GetDocumentKind(),
		SHA256: m.GetSha256(), SizeBytes: int32(len(xml)), CreatedBy: actor, CreatedAt: createdAt,
		Filename: safeFilename(invoiceNumber(inv.Payload), m.GetDocumentKind()),
	}, nil
}

// Get returns one export's metadata (db.ErrNotFound when it does not exist for the Firm).
func (s *Service) Get(ctx context.Context, firmID, exportID uuid.UUID) (View, error) {
	v, _, err := s.get(ctx, firmID, exportID)
	return v, err
}

func (s *Service) get(ctx context.Context, firmID, exportID uuid.UUID) (View, string, error) {
	var (
		v   View
		key string
	)
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		row, err := q.TrackCGetExport(ctx, exportID)
		if err != nil {
			return err
		}
		v = viewOf(row.ID, row.InvoiceID, row.RunID, row.PayloadVersion, row.RulesetVersion, row.Format,
			row.DocumentKind, row.Sha256, row.SizeBytes, row.CreatedBy, row.CreatedAt.Time, row.InvoiceNumber)
		key = row.ObjectKey
		return nil
	})
	return v, key, err
}

// List returns an invoice's exports, newest first.
func (s *Service) List(ctx context.Context, firmID, invoiceID uuid.UUID) ([]View, error) {
	out := []View{}
	err := db.WithFirm(ctx, s.Pool, firmID, func(q *sqlc.Queries) error {
		rows, err := q.TrackCListExports(ctx, invoiceID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			out = append(out, viewOf(r.ID, r.InvoiceID, r.RunID, r.PayloadVersion, r.RulesetVersion, r.Format,
				r.DocumentKind, r.Sha256, r.SizeBytes, r.CreatedBy, r.CreatedAt.Time, r.InvoiceNumber))
		}
		return nil
	})
	return out, err
}

// Read returns the stored document after checking its size and sha256 against the row
// (ErrCorrupt on mismatch).
func (s *Service) Read(ctx context.Context, firmID, exportID uuid.UUID) ([]byte, View, error) {
	v, key, err := s.get(ctx, firmID, exportID)
	if err != nil {
		return nil, View{}, err
	}
	b, err := s.Store.Get(ctx, key)
	if err != nil {
		return nil, View{}, err
	}
	if int64(len(b)) != int64(v.SizeBytes) || hexSHA256(b) != v.SHA256 {
		return nil, View{}, fmt.Errorf("%w: export %s", ErrCorrupt, exportID)
	}
	return b, v, nil
}

func viewOf(id, invoiceID, runID uuid.UUID, payloadVersion int32, ruleset, format, kind, sha string, size int32,
	createdBy string, createdAt time.Time, invoiceNumber string) View {
	return View{
		ID: id, InvoiceID: invoiceID, RunID: runID, PayloadVersion: payloadVersion, RulesetVersion: ruleset,
		Format: format, DocumentKind: kind, SHA256: sha, SizeBytes: size, CreatedBy: createdBy, CreatedAt: createdAt,
		Filename: safeFilename(invoiceNumber, kind),
	}
}

// invoiceNumber reads invoice_number from a stored payload (empty when absent).
func invoiceNumber(payload []byte) string {
	var inv compliancev1.Invoice
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(payload, &inv); err != nil {
		return ""
	}
	return inv.GetInvoiceNumber()
}

// safeFilename is "<invoice number>-<kind>.xml" restricted to [A-Za-z0-9._-]: every other rune
// becomes "_" and the number is cut to 100 characters, so the value is safe in Content-Disposition.
func safeFilename(number, kind string) string {
	clean := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
				b.WriteRune(r)
			default:
				b.WriteByte('_')
			}
		}
		return b.String()
	}
	n := clean(number)
	if len(n) > 100 {
		n = n[:100]
	}
	if n == "" {
		n = "invoice"
	}
	return n + "-" + clean(kind) + ".xml"
}
