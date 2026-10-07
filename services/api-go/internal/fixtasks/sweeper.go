package fixtasks

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

// Defaults of the sweeper (spec 5.6.8).
const (
	DefaultInterval     = 30 * time.Second
	DefaultPublishAfter = 2 * time.Minute  // a task still unpublished this long after its request is re-published
	DefaultTimeoutAfter = 30 * time.Minute // a task still requested this long after its request failed (timeout)
	sweepBatch          = 100
)

// Sweeper repairs what the request path cannot: it re-publishes tasks whose publish failed and closes
// tasks the agent never answered.
type Sweeper struct {
	Pool         *pgxpool.Pool
	Req          *Requester
	Interval     time.Duration
	PublishAfter time.Duration
	TimeoutAfter time.Duration
}

func orDefault(d, def time.Duration) time.Duration {
	if d <= 0 {
		return def
	}
	return d
}

// Run sweeps once at start and then every Interval until ctx is cancelled.
func (s *Sweeper) Run(ctx context.Context) {
	t := time.NewTicker(orDefault(s.Interval, DefaultInterval))
	defer t.Stop()
	for {
		if republished, closed, err := s.Sweep(ctx); err != nil {
			slog.WarnContext(ctx, "fixtasks: sweep failed", "err", err)
		} else if republished+closed > 0 {
			slog.InfoContext(ctx, "fixtasks: sweep", "republished", republished, "timed_out", closed)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Sweep is one pass over every Firm: first it closes tasks older than TimeoutAfter as failed/timeout,
// then it re-publishes the unpublished ones older than PublishAfter. A failing task is logged and left
// for the next pass.
func (s *Sweeper) Sweep(ctx context.Context) (republished, timedOut int, err error) {
	firms, err := sqlc.New(s.Pool).TrackCListFirmIDs(ctx) // firms is a global table
	if err != nil {
		return 0, 0, fmt.Errorf("list firms: %w", err)
	}
	now := time.Now()
	cutTimeout := pgtype.Timestamptz{Time: now.Add(-orDefault(s.TimeoutAfter, DefaultTimeoutAfter)), Valid: true}
	cutPublish := pgtype.Timestamptz{Time: now.Add(-orDefault(s.PublishAfter, DefaultPublishAfter)), Valid: true}
	for _, firm := range firms {
		if ctx.Err() != nil {
			return republished, timedOut, ctx.Err()
		}
		var closed []sqlc.TrackCTimeOutFixTasksRow
		var pending []sqlc.TrackCListUnpublishedFixTasksRow
		if err := db.WithFirm(ctx, s.Pool, firm, func(q *sqlc.Queries) error {
			var qerr error
			if closed, qerr = q.TrackCTimeOutFixTasks(ctx, cutTimeout); qerr != nil {
				return qerr
			}
			pending, qerr = q.TrackCListUnpublishedFixTasks(ctx, sqlc.TrackCListUnpublishedFixTasksParams{
				RequestedBefore: cutPublish, MaxRows: sweepBatch})
			return qerr
		}); err != nil {
			slog.WarnContext(ctx, "fixtasks: sweep firm", "firm_id", firm, "err", err)
			continue
		}
		timedOut += len(closed)
		for _, c := range closed {
			slog.InfoContext(ctx, "fixtasks: task timed out", "firm_id", firm, "task_id", c.ID, "invoice_id", c.InvoiceID)
		}
		for _, p := range pending {
			if p.Status != "requested" {
				continue
			}
			t := taskOf(sqlc.TrackCGetFixTaskRow(p))
			if err := s.Req.republish(ctx, firm, t); err != nil {
				slog.WarnContext(ctx, "fixtasks: republish failed", "firm_id", firm, "task_id", p.ID, "err", err)
				continue
			}
			republished++
		}
	}
	return republished, timedOut, nil
}
