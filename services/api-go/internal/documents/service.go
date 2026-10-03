package documents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/storage"
)

// Presigner signs browser URLs (*storage.Presigner implements it).
type Presigner interface {
	PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (*url.URL, http.Header, error)
	PresignGet(ctx context.Context, key, filename string, ttl time.Duration) (*url.URL, error)
}

// Objects reads and deletes stored objects (*storage.ObjectStore implements it).
type Objects interface {
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, key string) error
}

// Service implements the upload path of spec section 5.1.
type Service struct {
	Store     Store
	Presigner Presigner
	Objects   Objects
	Bus       events.ProtoPublisher
	Now       func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Upload is the signed PUT the browser performs.
type Upload struct {
	URL       string            `json:"url"`
	Method    string            `json:"method"`
	Headers   map[string]string `json:"headers"`
	ExpiresAt time.Time         `json:"expires_at"`
}

// UploadItem is one entry of the POST /v1/documents/uploads response (request order).
type UploadItem struct {
	DocumentID   *uuid.UUID `json:"document_id"`
	Status       string     `json:"status"`
	Deduplicated bool       `json:"deduplicated"`
	Upload       *Upload    `json:"upload"`
	Error        string     `json:"error,omitempty"`
}

// CompleteItem is one entry of the POST /v1/documents/complete response.
type CompleteItem struct {
	DocumentID string `json:"document_id"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// RequestUploads validates each file, deduplicates on (client company, sha256) and signs a PUT for
// every Document still waiting for its bytes. A duplicate (in the request or already stored) gets
// deduplicated=true and no upload, and nothing else happens (AC-E3a).
func (s *Service) RequestUploads(ctx context.Context, firmID, clientCompanyID uuid.UUID, uploadedBy string,
	files []FileIn) ([]UploadItem, error) {
	items := make([]UploadItem, len(files))
	var valid []FileIn
	var slots []int
	firstBySHA := map[string]int{}
	dupOf := map[int]int{}
	for i, f := range files {
		clean, code := f.Validate()
		if code != "" {
			items[i].Error = code
			continue
		}
		if j, seen := firstBySHA[clean.SHA256]; seen {
			dupOf[i] = j
			continue
		}
		firstBySHA[clean.SHA256] = i
		valid = append(valid, clean)
		slots = append(slots, i)
	}
	if len(valid) > 0 {
		pending, err := s.Store.PreparePending(ctx, firmID, clientCompanyID, uploadedBy, valid)
		if err != nil {
			return nil, err
		}
		expires := s.now().Add(PutTTL).UTC().Truncate(time.Second)
		for k, p := range pending {
			id := p.Doc.ID
			item := UploadItem{DocumentID: &id, Status: p.Doc.Status, Deduplicated: p.Deduplicated}
			if !p.Deduplicated {
				u, hdr, err := s.Presigner.PresignPut(ctx, p.Doc.ObjectKey, p.Doc.ContentType, p.Doc.SizeBytes, PutTTL)
				if err != nil {
					return nil, fmt.Errorf("presign %s: %w", p.Doc.ID, err)
				}
				headers := make(map[string]string, len(hdr))
				for k := range hdr { // every header the URL signed: the browser must send all of them
					headers[k] = hdr.Get(k)
				}
				item.Upload = &Upload{URL: u.String(), Method: http.MethodPut, Headers: headers, ExpiresAt: expires}
			}
			items[slots[k]] = item
		}
	}
	for i, j := range dupOf {
		first := items[j]
		items[i] = UploadItem{DocumentID: first.DocumentID, Status: first.Status, Deduplicated: true}
	}
	return items, nil
}

// Complete verifies each uploaded object (size, sha256, magic bytes), moves the Document to uploaded or
// rejected, and publishes document.uploaded. Repeating complete is harmless: only the call that moved
// the row publishes.
func (s *Service) Complete(ctx context.Context, firmID uuid.UUID, ids []string) []CompleteItem {
	out := make([]CompleteItem, len(ids))
	for i, raw := range ids {
		out[i] = s.completeOne(ctx, firmID, raw)
	}
	return out
}

func (s *Service) completeOne(ctx context.Context, firmID uuid.UUID, raw string) CompleteItem {
	item := CompleteItem{DocumentID: raw}
	id, err := uuid.Parse(raw)
	if err != nil {
		item.Error = CodeNotFound
		return item
	}
	doc, err := s.Store.Get(ctx, firmID, id)
	if errors.Is(err, db.ErrNotFound) {
		item.Error = CodeNotFound
		return item
	}
	if err != nil {
		slog.ErrorContext(ctx, "complete: get document", "document_id", id, "err", err)
		item.Error = "internal"
		return item
	}
	if doc.Status != "pending_upload" {
		item.Status = doc.Status
		return item
	}
	reason, err := s.verify(ctx, doc)
	if err != nil {
		slog.WarnContext(ctx, "complete: object store", "document_id", id, "err", err)
		item.Status, item.Error = doc.Status, CodeStorageUnavailable
		return item
	}
	doc, moved, err := s.Store.FinishUpload(ctx, firmID, id, reason)
	if err != nil {
		slog.ErrorContext(ctx, "complete: finish upload", "document_id", id, "err", err)
		item.Error = "internal"
		return item
	}
	item.Status = doc.Status
	if reason != "" {
		item.Error = reason
		if moved {
			if err := s.Objects.Delete(ctx, doc.ObjectKey); err != nil {
				slog.WarnContext(ctx, "complete: delete rejected object", "document_id", id, "err", err)
			}
		}
		return item
	}
	if moved {
		if err := s.PublishUploaded(ctx, doc); err != nil {
			// The row is committed with published_at NULL: the reconciler republishes it (spec 5.1).
			slog.WarnContext(ctx, "complete: publish document.uploaded", "document_id", id, "err", err)
		}
	}
	return item
}

// verify returns a rejection reason ("" = the object is exactly what was declared) or a storage error.
func (s *Service) verify(ctx context.Context, doc sqlc.Document) (string, error) {
	rc, size, err := s.Objects.Open(ctx, doc.ObjectKey)
	if errors.Is(err, storage.ErrObjectMissing) {
		return RejectObjectMissing, nil
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	if size != doc.SizeBytes {
		return RejectSizeMismatch, nil
	}
	h := sha256.New()
	head := make([]byte, 0, sniffBytes)
	buf := make([]byte, 32<<10)
	var n int64
	for {
		k, rerr := rc.Read(buf)
		if k > 0 {
			n += int64(k)
			if n > doc.SizeBytes {
				return RejectSizeMismatch, nil
			}
			if room := sniffBytes - len(head); room > 0 {
				head = append(head, buf[:min(room, k)]...)
			}
			_, _ = h.Write(buf[:k])
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("read %s: %w", doc.ObjectKey, rerr)
		}
	}
	switch {
	case n != doc.SizeBytes:
		return RejectSizeMismatch, nil
	case hex.EncodeToString(h.Sum(nil)) != doc.Sha256:
		return RejectSHA256Mismatch, nil
	case !SniffMatches(doc.ContentType, head):
		return RejectContentTypeMismatch, nil
	}
	return "", nil
}

// PublishUploaded publishes document.uploaded for an uploaded Document (with the Firm's active
// ClientCompanies as candidates) and records the ack. Task 17's reprocess and reconciler reuse it.
func (s *Service) PublishUploaded(ctx context.Context, doc sqlc.Document) error {
	cands, err := s.Store.Candidates(ctx, doc.FirmID)
	if err != nil {
		return err
	}
	msg := &compliancev1.DocumentUploaded{
		DocumentId: doc.ID.String(), FirmId: doc.FirmID.String(), ClientCompanyId: doc.ClientCompanyID.String(),
		Sha256: doc.Sha256, ObjectKey: doc.ObjectKey, ContentType: doc.ContentType, SizeBytes: doc.SizeBytes,
		Filename: doc.Filename, Candidates: cands, ReprocessNonce: doc.ReprocessNonce,
	}
	msgID := events.DocumentUploadedMsgID(doc.ID.String(), doc.ReprocessNonce)
	if err := s.Bus.Publish(ctx, events.DocumentUploadedSubject, msgID, msg); err != nil {
		return err
	}
	return s.Store.MarkPublished(ctx, doc.FirmID, doc.ID, doc.ReprocessNonce)
}

// Detail returns one Document with its invoices.
func (s *Service) Detail(ctx context.Context, firmID, id uuid.UUID) (DetailView, error) {
	doc, err := s.Store.Get(ctx, firmID, id)
	if err != nil {
		return DetailView{}, err
	}
	inv, err := s.Store.Invoices(ctx, firmID, id)
	if err != nil {
		return DetailView{}, err
	}
	return DetailView{View: ViewOf(doc), Invoices: inv}, nil
}

// Reprocess moves a Document back to uploaded and republishes document.uploaded (POST
// /v1/documents/{id}/reprocess). ErrNotReprocessable when the Document's current status does not
// allow it; db.ErrNotFound when it does not exist in this Firm.
func (s *Service) Reprocess(ctx context.Context, firm, id uuid.UUID) (string, error) {
	if _, err := s.Store.Get(ctx, firm, id); err != nil {
		return "", err
	}
	doc, err := s.Store.Reprocess(ctx, firm, id)
	if err != nil {
		return "", err
	}
	if err := s.PublishUploaded(ctx, doc); err != nil {
		// The row is committed as uploaded with published_at NULL: the reconciler republishes it.
		slog.WarnContext(ctx, "reprocess: publish document.uploaded", "document_id", id, "err", err)
	}
	return doc.Status, nil
}

// Download signs a 5-minute GET for a Document whose bytes were verified.
func (s *Service) Download(ctx context.Context, firmID, id uuid.UUID) (string, time.Time, error) {
	doc, err := s.Store.Get(ctx, firmID, id)
	if err != nil {
		return "", time.Time{}, err
	}
	if doc.Status == "pending_upload" || doc.Status == "rejected" {
		return "", time.Time{}, ErrNotDownloadable
	}
	u, err := s.Presigner.PresignGet(ctx, doc.ObjectKey, doc.Filename, GetTTL)
	if err != nil {
		return "", time.Time{}, err
	}
	return u.String(), s.now().Add(GetTTL).UTC().Truncate(time.Second), nil
}
