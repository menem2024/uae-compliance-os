package documents

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

// maxPublishAttempts is the number of times the reconciler retries a stuck document.uploaded publish
// before giving up and marking the Document failed (Task 17 design decisions).
const maxPublishAttempts = 3

// defaultReconcileInterval is Reconciler.Interval's default.
const defaultReconcileInterval = 60 * time.Second

// Reconciler recovers from crashes between a commit and its document.uploaded publish ack, and from
// Documents stuck processing after a lost agent.run.started/finished pair. One pass (Tick) covers
// every Firm; Run loops Tick on Interval until ctx is cancelled.
type Reconciler struct {
	Firms    FirmLister
	Store    Store
	Service  *Service
	Bus      events.ProtoPublisher
	Now      func() time.Time
	Interval time.Duration
}

// Run loops Tick on r.Interval (default 60s) until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = defaultReconcileInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := r.Tick(ctx); err != nil {
				slog.ErrorContext(ctx, "reconciler tick failed", "err", err)
			}
		}
	}
}

// Tick runs one pass over every Firm. A failure for one Firm (or one Document) never stops the rest.
func (r *Reconciler) Tick(ctx context.Context) error {
	firms, err := r.Firms.FirmIDs(ctx)
	if err != nil {
		return fmt.Errorf("list firms: %w", err)
	}
	for _, firm := range firms {
		r.tickFirm(ctx, firm)
	}
	return nil
}

func (r *Reconciler) tickFirm(ctx context.Context, firm uuid.UUID) {
	if _, err := r.Store.ResetStuck(ctx, firm); err != nil {
		slog.ErrorContext(ctx, "reconciler: reset stuck", "firm_id", firm, "err", err)
	}
	docs, err := r.Store.Unpublished(ctx, firm)
	if err != nil {
		slog.ErrorContext(ctx, "reconciler: list unpublished", "firm_id", firm, "err", err)
		return
	}
	for _, doc := range docs {
		r.tickDoc(ctx, firm, doc)
	}
}

func (r *Reconciler) tickDoc(ctx context.Context, firm uuid.UUID, doc sqlc.Document) {
	attempts, err := r.Store.BumpPublishAttempt(ctx, firm, doc.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return // another path (Complete, a concurrent reconciler pass) already finished it
	}
	if err != nil {
		slog.ErrorContext(ctx, "reconciler: bump publish attempt", "document_id", doc.ID, "err", err)
		return
	}
	if attempts > maxPublishAttempts {
		r.giveUp(ctx, firm, doc)
		return
	}
	if err := r.Service.PublishUploaded(ctx, doc); err != nil {
		slog.WarnContext(ctx, "reconciler: publish uploaded", "document_id", doc.ID, "err", err)
	}
}

func (r *Reconciler) giveUp(ctx context.Context, firm uuid.UUID, doc sqlc.Document) {
	if err := r.Store.FailUnpublished(ctx, firm, doc.ID); err != nil {
		slog.ErrorContext(ctx, "reconciler: fail unpublished", "document_id", doc.ID, "err", err)
		return
	}
	msg := &compliancev1.DocumentFailed{DocumentId: doc.ID.String(), FirmId: firm.String(), ReasonCode: "publish_exhausted"}
	msgID := events.DocumentFailedSubject + ":" + doc.ID.String() + ":" + doc.ReprocessNonce
	if err := r.Bus.Publish(ctx, events.DocumentFailedSubject, msgID, msg); err != nil {
		slog.ErrorContext(ctx, "reconciler: publish document.failed", "document_id", doc.ID, "err", err)
	}
}
