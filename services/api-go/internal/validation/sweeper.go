package validation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// sweepBatch is the most invoices re-validated per Firm per pass.
const sweepBatch = 100

// Sweeper re-validates invoices left in status "fixed" (spec §5.6.2). It closes the gap when the
// validator was unavailable right after an accepted fix or a correction.
type Sweeper struct {
	Svc      *Service
	Pool     *pgxpool.Pool
	Interval time.Duration // time between passes (default 30s)
	MinAge   time.Duration // a "fixed" invoice must be older than this (default 30s)
}

func (w *Sweeper) interval() time.Duration {
	if w.Interval <= 0 {
		return 30 * time.Second
	}
	return w.Interval
}

func (w *Sweeper) minAge() time.Duration {
	if w.MinAge <= 0 {
		return 30 * time.Second
	}
	return w.MinAge
}

// Run sweeps once at start and then every Interval until ctx is cancelled.
func (w *Sweeper) Run(ctx context.Context) {
	t := time.NewTicker(w.interval())
	defer t.Stop()
	for {
		if n, err := w.Sweep(ctx); err != nil {
			slog.WarnContext(ctx, "validation sweep failed", "err", err)
		} else if n > 0 {
			slog.InfoContext(ctx, "validation sweep", "revalidated", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Sweep is one pass over every Firm. It returns the number of invoices re-validated. A failing
// invoice is logged and left for the next pass; it never stops the pass.
func (w *Sweeper) Sweep(ctx context.Context) (int, error) {
	firms, err := sqlc.New(w.Pool).TrackCListFirmIDs(ctx) // firms is a global table
	if err != nil {
		return 0, fmt.Errorf("list firms: %w", err)
	}
	cutoff := time.Now().Add(-w.minAge())
	done := 0
	for _, firm := range firms {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		var fixed []sqlc.TrackCListFixedInvoicesRow
		if err := db.WithFirm(ctx, w.Pool, firm, func(q *sqlc.Queries) error {
			var qerr error
			fixed, qerr = q.TrackCListFixedInvoices(ctx, sqlc.TrackCListFixedInvoicesParams{
				UpdatedBefore: pgtype.Timestamptz{Time: cutoff, Valid: true}, MaxRows: sweepBatch,
			})
			return qerr
		}); err != nil {
			slog.WarnContext(ctx, "list fixed invoices", "firm_id", firm, "err", err)
			continue
		}
		for _, inv := range fixed {
			if _, err := w.Svc.Run(ctx, firm, inv.ID, RunOpts{Trigger: TriggerSweeper}); err != nil {
				slog.WarnContext(ctx, "sweeper revalidation failed", "firm_id", firm, "invoice_id", inv.ID, "err", err)
				continue
			}
			done++
		}
	}
	return done, nil
}
