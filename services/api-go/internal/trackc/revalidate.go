package trackc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// revalidatePage is how many invoices one Firm query returns.
const revalidatePage = 200

// RevalidateOpts configures Revalidate (`api revalidate`, spec §5.6.2).
type RevalidateOpts struct {
	Pool    *pgxpool.Pool
	Svc     *validation.Service
	Ruleset string    // the RuleSet id to move every invoice to
	Firm    uuid.UUID // uuid.Nil = every Firm
	Rate    int       // invoices per second (> 0)
	Report  io.Writer // JSONL, one line per re-validated invoice; nil = none
}

// Totals is the outcome of Revalidate.
type Totals struct {
	Invoices      int // re-validated successfully
	Failed        int // left on their old run; a later pass retries them
	IssuesAdded   int
	IssuesRemoved int
	StatusChanges map[string]int // "from->to" for invoices whose status changed
}

// Print writes the totals in human-readable form.
func (t Totals) Print(w io.Writer) {
	_, _ = fmt.Fprintf(w, "invoices: %d\nfailed: %d\nissues added: %d\nissues removed: %d\n", t.Invoices, t.Failed, t.IssuesAdded, t.IssuesRemoved)
	keys := make([]string, 0, len(t.StatusChanges))
	for k := range t.StatusChanges {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(w, "status %s: %d\n", strings.Replace(k, "->", " -> ", 1), t.StatusChanges[k])
	}
}

// issueKey identifies an issue in a diff.
type issueKey struct {
	RuleID   string `json:"rule_id"`
	Path     string `json:"path"`
	Severity string `json:"severity"`
}

type reportLine struct {
	InvoiceID  uuid.UUID  `json:"invoice_id"`
	FromRunID  uuid.UUID  `json:"from_run_id"`
	ToRunID    uuid.UUID  `json:"to_run_id"`
	FromStatus string     `json:"from_status"`
	ToStatus   string     `json:"to_status"`
	Added      []issueKey `json:"added"`
	Removed    []issueKey `json:"removed"`
}

// Revalidate re-validates every invoice whose latest run used a RuleSet other than o.Ruleset
// (trigger ruleset_upgrade), ADR 010's re-validation. A failing invoice is counted and logged and
// never stops the pass. It stops on ctx cancellation or when the report cannot be written.
func Revalidate(ctx context.Context, o RevalidateOpts) (Totals, error) {
	tot := Totals{StatusChanges: map[string]int{}}
	switch {
	case o.Ruleset == "":
		return tot, errors.New("revalidate: --ruleset is required")
	case o.Rate <= 0:
		return tot, errors.New("revalidate: --rate must be positive")
	case o.Pool == nil || o.Svc == nil:
		return tot, errors.New("revalidate: pool and validation service are required")
	}
	firms := []uuid.UUID{o.Firm}
	if o.Firm == uuid.Nil {
		var err error
		if firms, err = sqlc.New(o.Pool).TrackCListFirmIDs(ctx); err != nil { // firms is a global table
			return tot, fmt.Errorf("revalidate: list firms: %w", err)
		}
	}
	tick := time.NewTicker(time.Second / time.Duration(o.Rate))
	defer tick.Stop()
	for _, firm := range firms {
		after := uuid.Nil
		for {
			var page []sqlc.TrackCListInvoicesForRevalidationRow
			if err := db.WithFirm(ctx, o.Pool, firm, func(q *sqlc.Queries) error {
				var err error
				page, err = q.TrackCListInvoicesForRevalidation(ctx, sqlc.TrackCListInvoicesForRevalidationParams{
					RulesetVersion: o.Ruleset, AfterID: after, MaxRows: revalidatePage,
				})
				return err
			}); err != nil {
				return tot, fmt.Errorf("revalidate: list invoices of firm %s: %w", firm, err)
			}
			for _, inv := range page {
				after = inv.ID
				select {
				case <-ctx.Done():
					return tot, ctx.Err()
				case <-tick.C:
				}
				line, err := revalidateOne(ctx, o, firm, inv)
				if err != nil {
					tot.Failed++
					slog.WarnContext(ctx, "revalidate failed", "firm_id", firm, "invoice_id", inv.ID, "err", err)
					continue
				}
				tot.Invoices++
				tot.IssuesAdded += len(line.Added)
				tot.IssuesRemoved += len(line.Removed)
				if line.FromStatus != line.ToStatus {
					tot.StatusChanges[line.FromStatus+"->"+line.ToStatus]++
				}
				if o.Report != nil {
					b, err := json.Marshal(line)
					if err != nil {
						return tot, err
					}
					if _, err := o.Report.Write(append(b, '\n')); err != nil {
						return tot, fmt.Errorf("revalidate: write report: %w", err)
					}
				}
			}
			if len(page) < revalidatePage {
				break
			}
		}
	}
	return tot, nil
}

func revalidateOne(ctx context.Context, o RevalidateOpts, firm uuid.UUID, inv sqlc.TrackCListInvoicesForRevalidationRow) (reportLine, error) {
	var old []sqlc.TrackCListIssuesRow
	if err := db.WithFirm(ctx, o.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		old, err = q.TrackCListIssues(ctx, inv.RunID)
		return err
	}); err != nil {
		return reportLine{}, fmt.Errorf("read issues of run %s: %w", inv.RunID, err)
	}
	res, err := o.Svc.Run(ctx, firm, inv.ID, validation.RunOpts{Trigger: validation.TriggerRulesetUpgrade, RulesetVersion: o.Ruleset})
	if err != nil {
		return reportLine{}, err
	}
	oldSet := map[issueKey]bool{}
	for _, i := range old {
		oldSet[issueKey{i.RuleID, i.Path, i.Severity}] = true
	}
	newSet := map[issueKey]bool{}
	for _, i := range res.Issues {
		newSet[issueKey{i.GetRuleId(), i.GetPath(), severityName(i.GetSeverity())}] = true
	}
	line := reportLine{InvoiceID: inv.ID, FromRunID: inv.RunID, ToRunID: res.RunID, FromStatus: inv.Status, ToStatus: res.Status,
		Added: []issueKey{}, Removed: []issueKey{}}
	for k := range newSet {
		if !oldSet[k] {
			line.Added = append(line.Added, k)
		}
	}
	for k := range oldSet {
		if !newSet[k] {
			line.Removed = append(line.Removed, k)
		}
	}
	sortKeys(line.Added)
	sortKeys(line.Removed)
	return line, nil
}

func sortKeys(k []issueKey) {
	sort.Slice(k, func(i, j int) bool {
		a, b := k[i], k[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Severity < b.Severity
	})
}

func severityName(s compliancev1.Severity) string {
	if s == compliancev1.Severity_SEVERITY_WARNING {
		return "warning"
	}
	return "error"
}
