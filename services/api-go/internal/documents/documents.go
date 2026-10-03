// Package documents is Track B's Document ingestion: signed uploads with sha256 dedup, server-side
// verification on complete, listing and download (spec section 5.1 steps 1-4), plus the results
// consumer, reprocess and reconciler (Task 17).
package documents

import (
	"bytes"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// Upload limits (spec section 5.8, Global Constraints).
const (
	MaxFiles        = 100
	MaxSizeBytes    = 20 << 20 // 20 MiB
	MaxFilename     = 255
	PutTTL          = 15 * time.Minute
	GetTTL          = 5 * time.Minute
	DefaultLimit    = 50
	MaxPageLimit    = 100
	sniffBytes      = 512
	ContentTypeCSV  = "text/csv"
	ContentTypeXLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

// ContentTypes is the upload allowlist.
var ContentTypes = map[string]bool{
	"application/pdf": true, "image/png": true, "image/jpeg": true, "image/webp": true,
	ContentTypeCSV: true, ContentTypeXLSX: true,
}

// Statuses is the documents.status value set (migration 00012).
var Statuses = map[string]bool{
	"pending_upload": true, "uploaded": true, "processing": true, "extracted": true, "needs_review": true,
	"not_invoice": true, "failed": true, "rejected": true,
}

// Per-item and request error codes.
const (
	CodeInvalidFilename       = "invalid_filename"
	CodeInvalidContentType    = "invalid_content_type"
	CodeInvalidSize           = "invalid_size"
	CodeInvalidSHA256         = "invalid_sha256"
	CodeNotFound              = "not_found"
	CodeStorageUnavailable    = "storage_unavailable"
	RejectObjectMissing       = "object_missing"
	RejectSizeMismatch        = "size_mismatch"
	RejectSHA256Mismatch      = "sha256_mismatch"
	RejectContentTypeMismatch = "content_type_mismatch"
)

var (
	// ErrClientNotFound: the ClientCompany does not exist in the caller's Firm.
	ErrClientNotFound = errors.New("client company not found")
	// ErrClientArchived: uploads go only to active ClientCompanies.
	ErrClientArchived = errors.New("client company archived")
	// ErrNotDownloadable: the object of a pending or rejected Document may not exist.
	ErrNotDownloadable = errors.New("document not downloadable")
	// ErrNotReprocessable: the Document's current status does not allow a reprocess (Task 17).
	ErrNotReprocessable = errors.New("document not reprocessable")
)

// LowConfidence is the per-invoice confidence floor below which status is needs_review (Task 13's
// rule, applied by the results consumer).
const LowConfidence = 0.85

// DocumentStatus is the Document-level status rule (Task 17 design decisions): a Document whose kind
// is not an invoice or credit_note is not_invoice regardless of needsReview.
func DocumentStatus(kind string, needsReview bool) string {
	if kind != "invoice" && kind != "credit_note" {
		return "not_invoice"
	}
	if needsReview {
		return "needs_review"
	}
	return "extracted"
}

// InvoiceStatus is the per-invoice status rule (Task 13's rule, applied here): anything short of a
// clean VERDICT_ACCEPT at or above LowConfidence needs a human.
func InvoiceStatus(verdict compliancev1.Verdict, confidence float64) string {
	if verdict != compliancev1.Verdict_VERDICT_ACCEPT || confidence < LowConfidence {
		return "needs_review"
	}
	return "extracted"
}

// ExtractedParams is the Document-level fields of one document.extracted result (Store.ApplyExtractedResult).
type ExtractedParams struct {
	ID    uuid.UUID
	RunID uuid.UUID
	// ClientCompanyID is the company ai-py extracted for (it echoes document.uploaded). It is only
	// compared with the locked Document's; the invoices always take the Document's, never this one.
	ClientCompanyID  uuid.UUID
	Status           string
	StatusReason     string
	Kind             string
	Direction        string
	Language         string
	ExtractionMethod string
	ReviewReasons    []string
	InvoiceCount     int32
}

// InvoiceIn is one invoice to insert for a document.extracted result (Store.ApplyExtractedResult).
type InvoiceIn struct {
	SourceOrdinal int32
	SourceRef     string
	Payload       []byte
	Status        string
	Confidence    float64
}

// InvoiceOut is one invoice of the Document's full invoice set, read back inside the same transaction as
// the insert so the consumer always decides what to publish from the authoritative row, not from what it
// tried to insert.
type InvoiceOut struct {
	ID         uuid.UUID
	Status     string
	Payload    []byte
	Confidence *float64
}

// ApplyOutcome is what Store.ApplyExtractedResult did.
type ApplyOutcome struct {
	// Found is false when the Document is not visible to the Firm (missing, or another Firm's).
	Found bool
	// Applied is true when this call moved the Document out of uploaded/processing.
	Applied bool
	// Replay is true when the Document already holds this very result (same run, same status): a
	// redelivery, whose invoice.extracted publish may have been lost after the first commit.
	Replay bool
	// Invoices is the Document's full invoice set when Applied or Replay (not for not_invoice).
	Invoices []InvoiceOut
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ObjectKey is the only object key shape (the documents_object_key_shape CHECK enforces it): no user
// input, and one Document can never overwrite another's object.
func ObjectKey(firmID, documentID uuid.UUID) string {
	return "firms/" + firmID.String() + "/docs/" + documentID.String()
}

// FileIn is one requested upload.
type FileIn struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

// CleanFilename strips control characters, bidi/format controls and surrounding space; filenames are
// display-only. Bidi controls (U+202A-202E, U+2066-2069, U+061C) and zero-width/direction marks
// (U+200B-200F, U+FEFF) are removed because U+202E makes "invoice\u202Efdp.exe" render as "invoiceexe.pdf".
func CleanFilename(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case unicode.IsControl(r), r == utf8.RuneError:
			return -1
		case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069,
			r == 0x061C, r == 0xFEFF:
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// contentTypeExt is the canonical extension of each allowed content type.
var contentTypeExt = map[string]string{
	"application/pdf": "pdf", "image/png": "png", "image/jpeg": "jpg", "image/webp": "webp",
	ContentTypeCSV: "csv", ContentTypeXLSX: "xlsx",
}

// DownloadFilename is the name a Document is downloaded under: the stored name (cleaned again, in case an
// older row predates CleanFilename's rules) with its extension forced to the verified content type's, so a
// colleague never saves "invoice.hta" that is really text/csv. A name that already ends in an accepted
// extension of that type is kept as is (".jpeg" for image/jpeg); a mismatching extension is replaced; a
// name with no extension gets one appended.
func DownloadFilename(name, contentType string) string {
	name = CleanFilename(name)
	want, ok := contentTypeExt[contentType]
	if !ok {
		return name
	}
	base, ext := name, ""
	if i := strings.LastIndexByte(name, '.'); i >= 0 && isExtension(name[i+1:]) {
		base, ext = name[:i], strings.ToLower(name[i+1:])
	} else if i >= 0 && i == len(name)-1 {
		base = name[:i] // "name." has an empty extension
	}
	if ext == want || (want == "jpg" && ext == "jpeg") {
		return name
	}
	if base == "" {
		base = "document"
	}
	return base + "." + want
}

// isExtension reports whether s looks like a file extension: 1-5 ASCII letters or digits.
func isExtension(s string) bool {
	if s == "" || len(s) > 5 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// Validate returns the cleaned file or a per-item error code.
func (f FileIn) Validate() (FileIn, string) {
	f.Filename = CleanFilename(f.Filename)
	switch {
	case f.Filename == "" || utf8.RuneCountInString(f.Filename) > MaxFilename:
		return f, CodeInvalidFilename
	case !ContentTypes[f.ContentType]:
		return f, CodeInvalidContentType
	case f.SizeBytes < 1 || f.SizeBytes > MaxSizeBytes:
		return f, CodeInvalidSize
	case !sha256Hex.MatchString(f.SHA256):
		return f, CodeInvalidSHA256
	}
	return f, ""
}

// SniffMatches reports whether the first bytes of an object fit its declared content type.
func SniffMatches(contentType string, head []byte) bool {
	switch contentType {
	case "application/pdf":
		return bytes.HasPrefix(head, []byte("%PDF-"))
	case "image/png":
		return bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n"))
	case "image/jpeg":
		return bytes.HasPrefix(head, []byte{0xFF, 0xD8, 0xFF})
	case "image/webp":
		return len(head) >= 12 && bytes.Equal(head[:4], []byte("RIFF")) && bytes.Equal(head[8:12], []byte("WEBP"))
	case ContentTypeXLSX:
		return bytes.HasPrefix(head, []byte("PK\x03\x04"))
	case ContentTypeCSV:
		return len(head) > 0 && !bytes.Contains(head, []byte{0}) && !bytes.HasPrefix(head, []byte("PK\x03\x04")) &&
			!bytes.HasPrefix(head, []byte("%PDF-"))
	}
	return false
}

// View is the JSON view of a Document.
type View struct {
	ID               uuid.UUID  `json:"id"`
	ClientCompanyID  uuid.UUID  `json:"client_company_id"`
	Filename         string     `json:"filename"`
	ContentType      string     `json:"content_type"`
	SizeBytes        int64      `json:"size_bytes"`
	SHA256           string     `json:"sha256"`
	Status           string     `json:"status"`
	StatusReason     string     `json:"status_reason"`
	Kind             string     `json:"kind"`
	Direction        string     `json:"direction"`
	Language         string     `json:"language"`
	ExtractionMethod string     `json:"extraction_method"`
	ReviewReasons    []string   `json:"review_reasons"`
	InvoiceCount     int32      `json:"invoice_count"`
	LatestRunID      *uuid.UUID `json:"latest_run_id"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// InvoiceRef is one invoice created from a Document.
type InvoiceRef struct {
	ID                   uuid.UUID `json:"id"`
	Status               string    `json:"status"`
	SourceOrdinal        int32     `json:"source_ordinal"`
	ExtractionConfidence *float64  `json:"extraction_confidence"`
}

// DetailView is GET /v1/documents/{id}.
type DetailView struct {
	View
	Invoices []InvoiceRef `json:"invoices"`
}

// ViewOf maps a row to its JSON view.
func ViewOf(d sqlc.Document) View {
	v := View{ID: d.ID, ClientCompanyID: d.ClientCompanyID, Filename: d.Filename, ContentType: d.ContentType,
		SizeBytes: d.SizeBytes, SHA256: d.Sha256, Status: d.Status, StatusReason: d.StatusReason, Kind: d.Kind,
		Direction: d.Direction, Language: d.Language, ExtractionMethod: d.ExtractionMethod,
		ReviewReasons: d.ReviewReasons, InvoiceCount: d.InvoiceCount, CreatedAt: d.CreatedAt.Time,
		UpdatedAt: d.UpdatedAt.Time}
	if v.ReviewReasons == nil {
		v.ReviewReasons = []string{}
	}
	if d.LatestRunID.Valid {
		id := d.LatestRunID.UUID
		v.LatestRunID = &id
	}
	return v
}

// ListFilter selects a page of Documents (newest first).
type ListFilter struct {
	ClientCompanyID uuid.UUID // uuid.Nil = all
	Status          string    // "" = all
	BeforeCreatedAt time.Time
	BeforeID        uuid.UUID // uuid.Nil = first page
	Limit           int
}
