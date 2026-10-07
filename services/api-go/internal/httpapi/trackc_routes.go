package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/exports"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/invoicefix"
	"github.com/menem2024/uae-platform/services/api-go/internal/trackc"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// Limits of the Track C routes.
const (
	maxTrackCBody   = 256 << 10
	defaultPageSize = 50
	maxPageSize     = 100
	maxQueryLen     = 100
	maxChanges      = 50
	maxReasonRunes  = 500
	maxRunsListed   = 100
	maxAuditListed  = 200
)

// invoiceStatuses is the invoices.status CHECK list.
var invoiceStatuses = map[string]bool{
	"uploaded": true, "classified": true, "extracted": true, "needs_review": true,
	"validated": true, "has_issues": true, "fixed": true, "ready": true,
}

// WithTrackC mounts the Track C routes (spec §5.6.7) inside the authenticated group. GET routes sit
// behind the c-read Firm stack and mutations behind c-write; every handler reads the Firm only from
// the context and touches tenant rows only through db.WithFirm. Mounts added by the module's wiring
// files (proposals, Task 19) are mounted with the same stacks.
func WithTrackC(m *trackc.Module) RouterOption {
	return func(r chi.Router) {
		h := &trackCHandlers{m: m}
		mw := trackc.Middlewares{
			Read:  trackc.FirmMiddleware(m.Firms, m.ReadLimiter),
			Write: trackc.FirmMiddleware(m.Firms, m.WriteLimiter),
		}
		r.With(mw.Read).Get("/v1/invoices", h.listInvoices)
		r.With(mw.Read).Get("/v1/invoices/{id}/validation", h.invoiceDetail)
		r.With(mw.Write).Post("/v1/invoices/{id}/validation", h.validateNow)
		r.With(mw.Read).Get("/v1/invoices/{id}/validation/runs", h.listRuns)
		r.With(mw.Read).Get("/v1/invoices/{id}/validation/runs/{run_id}", h.getRun)
		r.With(mw.Read).Get("/v1/invoices/{id}/validation/runs/{run_id}/diff", h.runDiff)
		r.With(mw.Write).Post("/v1/invoices/{id}/validation/corrections", h.corrections)
		r.With(mw.Write).Post("/v1/invoices/{id}/validation/approve", h.approve)
		r.With(mw.Write).Post("/v1/invoices/{id}/validation/fixes", h.requestFixes)
		r.With(mw.Read).Get("/v1/invoices/{id}/validation/audit", h.invoiceAudit)
		r.With(mw.Write).Post("/v1/exports", h.createExport)
		r.With(mw.Read).Get("/v1/exports", h.listExports)
		r.With(mw.Read).Get("/v1/exports/{id}", h.getExport)
		r.With(mw.Read).Get("/v1/exports/{id}/xml", h.downloadExport)
		for _, mount := range m.Mounts {
			mount(r, mw)
		}
	}
}

type trackCHandlers struct{ m *trackc.Module }

// ---- JSON shapes ----------------------------------------------------------------------------

type issueJSON struct {
	RuleID         string          `json:"rule_id"`
	Severity       string          `json:"severity"`
	Path           string          `json:"path"`
	BusinessTerm   string          `json:"business_term"`
	Message        string          `json:"message"`
	MessageAr      string          `json:"message_ar"`
	MessageArgs    json.RawMessage `json:"message_args"`
	Fixable        bool            `json:"fixable"`
	SuggestedValue string          `json:"suggested_value"`
}

type runJSON struct {
	ID             uuid.UUID    `json:"id"`
	PayloadVersion int32        `json:"payload_version"`
	RulesetVersion string       `json:"ruleset_version"`
	Trigger        string       `json:"trigger"`
	ErrorCount     int32        `json:"error_count"`
	WarningCount   int32        `json:"warning_count"`
	RulesEvaluated int32        `json:"rules_evaluated"`
	DurationUs     int64        `json:"duration_us"`
	RequestedBy    string       `json:"requested_by"`
	TraceID        string       `json:"trace_id"`
	CreatedAt      time.Time    `json:"created_at"`
	Issues         *[]issueJSON `json:"issues,omitempty"`
}

type diffKey struct {
	RuleID   string `json:"rule_id"`
	Path     string `json:"path"`
	Severity string `json:"severity"`
}

type fixTaskJSON struct {
	ID          uuid.UUID  `json:"id"`
	RunID       uuid.UUID  `json:"run_id"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	Outcome     string     `json:"outcome"`
	ProposalID  *uuid.UUID `json:"proposal_id"`
	ErrorCode   string     `json:"error_code"`
	RequestedAt time.Time  `json:"requested_at"`
	CompletedAt *time.Time `json:"completed_at"`
}

type exportJSON struct {
	ID             uuid.UUID `json:"id"`
	InvoiceID      uuid.UUID `json:"invoice_id"`
	RunID          uuid.UUID `json:"run_id"`
	PayloadVersion int32     `json:"payload_version"`
	RulesetVersion string    `json:"ruleset_version"`
	Format         string    `json:"format"`
	DocumentKind   string    `json:"document_kind"`
	SHA256         string    `json:"sha256"`
	SizeBytes      int32     `json:"size_bytes"`
	CreatedBy      string    `json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	Filename       string    `json:"filename"`
}

func ts(t pgtype.Timestamptz) time.Time { return t.Time.UTC() }

func optTS(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}

func optUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func rawOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return b
}

func severityString(s compliancev1.Severity) string {
	if s == compliancev1.Severity_SEVERITY_WARNING {
		return "warning"
	}
	return "error"
}

func issueFromRow(i sqlc.TrackCListIssuesRow) issueJSON {
	return issueJSON{RuleID: i.RuleID, Severity: i.Severity, Path: i.Path, BusinessTerm: i.BusinessTerm, Message: i.Message,
		MessageAr: i.MessageAr, MessageArgs: rawOrNull(i.MessageArgs), Fixable: i.Fixable, SuggestedValue: i.SuggestedValue}
}

func issueFromProto(i *compliancev1.ValidationIssue) issueJSON {
	args := i.GetMessageArgs()
	if args == nil {
		args = map[string]string{}
	}
	raw, _ := json.Marshal(args)
	return issueJSON{RuleID: i.GetRuleId(), Severity: severityString(i.GetSeverity()), Path: i.GetPath(), BusinessTerm: i.GetBusinessTerm(),
		Message: i.GetMessage(), MessageAr: i.GetMessageAr(), MessageArgs: raw, Fixable: i.GetFixable(), SuggestedValue: i.GetSuggestedValue()}
}

func runFromRow(r sqlc.TrackCGetRunRow) runJSON {
	return runJSON{ID: r.ID, PayloadVersion: r.PayloadVersion, RulesetVersion: r.RulesetVersion, Trigger: r.Trigger,
		ErrorCount: r.ErrorCount, WarningCount: r.WarningCount, RulesEvaluated: r.RulesEvaluated, DurationUs: r.DurationUs,
		RequestedBy: r.RequestedBy, TraceID: r.TraceID, CreatedAt: ts(r.CreatedAt)}
}

func runFromListRow(r sqlc.TrackCListRunsRow) runJSON { return runFromRow(sqlc.TrackCGetRunRow(r)) }

func issuesFromRows(rows []sqlc.TrackCListIssuesRow) []issueJSON {
	out := make([]issueJSON, 0, len(rows))
	for _, r := range rows {
		out = append(out, issueFromRow(r))
	}
	return out
}

func fixTaskFromRow(t sqlc.TrackCLatestFixTaskRow) *fixTaskJSON {
	return &fixTaskJSON{ID: t.ID, RunID: t.RunID, Mode: t.Mode, Status: t.Status, Outcome: t.Outcome,
		ProposalID: optUUID(t.ProposalID), ErrorCode: t.ErrorCode, RequestedAt: ts(t.RequestedAt), CompletedAt: optTS(t.CompletedAt)}
}

func exportJSONOf(v exports.View) exportJSON {
	return exportJSON{ID: v.ID, InvoiceID: v.InvoiceID, RunID: v.RunID, PayloadVersion: v.PayloadVersion, RulesetVersion: v.RulesetVersion,
		Format: v.Format, DocumentKind: v.DocumentKind, SHA256: v.SHA256, SizeBytes: v.SizeBytes, CreatedBy: v.CreatedBy,
		CreatedAt: v.CreatedAt.UTC(), Filename: v.Filename}
}

// ---- helpers --------------------------------------------------------------------------------

func tcError(w http.ResponseWriter, status int, code string) { trackc.WriteError(w, status, code) }

func tcInternal(ctx context.Context, w http.ResponseWriter, what string, err error) {
	slog.ErrorContext(ctx, "trackc: "+what, "err", err)
	tcError(w, http.StatusInternalServerError, "internal")
}

// pathID parses a URL id; anything that is not a uuid cannot name a row, so it is a 404.
func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		tcError(w, http.StatusNotFound, "not_found")
		return uuid.Nil, false
	}
	return id, true
}

func subject(r *http.Request) string {
	p, _ := auth.FromContext(r.Context())
	return p.Subject
}

// decodeBody strictly decodes one JSON object: unknown fields, trailing data and oversized bodies fail.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTrackCBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		tcError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		tcError(w, http.StatusBadRequest, "bad_request")
		return false
	}
	return true
}

// inFirm runs fn in a db.WithFirm transaction for the request's Firm. A db.ErrNotFound (a missing row
// or one hidden by RLS) answers 404 not_found and any other error 500 internal; it reports whether
// the handler should continue.
func (h *trackCHandlers) inFirm(w http.ResponseWriter, r *http.Request, what string, fn func(q *sqlc.Queries, firm uuid.UUID) error) bool {
	firm := trackc.FirmFrom(r.Context())
	err := db.WithFirm(r.Context(), h.m.Pool, firm, func(q *sqlc.Queries) error { return fn(q, firm) })
	switch {
	case err == nil:
		return true
	case errors.Is(err, db.ErrNotFound):
		tcError(w, http.StatusNotFound, "not_found")
	default:
		tcInternal(r.Context(), w, what, err)
	}
	return false
}

// ---- GET /v1/invoices -----------------------------------------------------------------------

func encodeCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func decodeCursor(c string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	at, rest, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, errors.New("cursor shape")
	}
	t, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, uuid.Nil, err
	}
	id, err := uuid.Parse(rest)
	return t, id, err
}

func (h *trackCHandlers) listInvoices(w http.ResponseWriter, r *http.Request) {
	qv := r.URL.Query()
	bad := func() { tcError(w, http.StatusBadRequest, "bad_query") }
	arg := sqlc.TrackCListInvoicesParams{}
	if s := qv.Get("status"); s != "" {
		if !invoiceStatuses[s] {
			bad()
			return
		}
		arg.Status = pgtype.Text{String: s, Valid: true}
	}
	if q := qv.Get("q"); q != "" {
		if utf8.RuneCountInString(q) > maxQueryLen {
			bad()
			return
		}
		arg.Q = pgtype.Text{String: q, Valid: true}
	}
	limit := defaultPageSize
	if s := qv.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxPageSize {
			bad()
			return
		}
		limit = n
	}
	if c := qv.Get("cursor"); c != "" {
		at, id, err := decodeCursor(c)
		if err != nil {
			bad()
			return
		}
		arg.BeforeCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		arg.BeforeID = id
	}
	arg.PageLimit = int32(limit) + 1 //nolint:gosec // limit <= maxPageSize

	var rows []sqlc.TrackCListInvoicesRow
	if !h.inFirm(w, r, "list invoices", func(q *sqlc.Queries, _ uuid.UUID) error {
		var err error
		rows, err = q.TrackCListInvoices(r.Context(), arg)
		return err
	}) {
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		c := encodeCursor(rows[limit-1].CreatedAt.Time, rows[limit-1].ID)
		next = &c
	}
	type item struct {
		ID              uuid.UUID  `json:"id"`
		Status          string     `json:"status"`
		PayloadVersion  int32      `json:"payload_version"`
		LatestRunID     *uuid.UUID `json:"latest_run_id"`
		InvoiceNumber   string     `json:"invoice_number"`
		IssueDate       string     `json:"issue_date"`
		InvoiceTypeCode string     `json:"invoice_type_code"`
		Currency        string     `json:"currency"`
		TotalAmount     string     `json:"total_amount"`
		SellerName      string     `json:"seller_name"`
		BuyerName       string     `json:"buyer_name"`
		ErrorCount      int32      `json:"error_count"`
		WarningCount    int32      `json:"warning_count"`
		CreatedAt       time.Time  `json:"created_at"`
		UpdatedAt       time.Time  `json:"updated_at"`
	}
	items := make([]item, 0, len(rows))
	for _, x := range rows {
		items = append(items, item{ID: x.ID, Status: x.Status, PayloadVersion: x.PayloadVersion, LatestRunID: optUUID(x.LatestRunID),
			InvoiceNumber: x.InvoiceNumber, IssueDate: x.IssueDate, InvoiceTypeCode: x.InvoiceTypeCode, Currency: x.Currency,
			TotalAmount: x.TotalAmount, SellerName: x.SellerName, BuyerName: x.BuyerName, ErrorCount: x.ErrorCount,
			WarningCount: x.WarningCount, CreatedAt: ts(x.CreatedAt), UpdatedAt: ts(x.UpdatedAt)})
	}
	trackc.WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

// ---- /v1/invoices/{id}/validation* ----------------------------------------------------------

func (h *trackCHandlers) invoiceDetail(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var (
		inv   sqlc.TrackCGetInvoiceRow
		run   *runJSON
		fixes *fixTaskJSON
		open  any // the open invoice.field_fix proposal, if any (trackc.OpenInvoiceProposal)
	)
	if !h.inFirm(w, r, "invoice detail", func(q *sqlc.Queries, _ uuid.UUID) error {
		var err error
		if inv, err = q.TrackCGetInvoice(ctx, id); err != nil {
			return err
		}
		if inv.LatestRunID != uuid.Nil {
			row, err := q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: inv.LatestRunID, InvoiceID: id})
			if err != nil {
				return err
			}
			issues, err := q.TrackCListIssues(ctx, inv.LatestRunID)
			if err != nil {
				return err
			}
			rj := runFromRow(row)
			is := issuesFromRows(issues)
			rj.Issues = &is
			run = &rj
		}
		t, err := q.TrackCLatestFixTask(ctx, id)
		switch {
		case err == nil:
			fixes = fixTaskFromRow(t)
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		open, err = trackc.OpenInvoiceProposal(ctx, q, id)
		return err
	}) {
		return
	}
	var approval map[string]any
	if inv.ApprovedPayloadVersion.Valid {
		approval = map[string]any{"approved_payload_version": inv.ApprovedPayloadVersion.Int32, "approved_by": inv.ApprovedBy.String,
			"approved_at": ts(inv.ApprovedAt)}
	}
	var ruleset *string
	if inv.RulesetVersion.Valid {
		ruleset = &inv.RulesetVersion.String
	}
	trackc.WriteJSON(w, http.StatusOK, map[string]any{
		"id": inv.ID, "status": inv.Status, "payload_version": inv.PayloadVersion, "payload": json.RawMessage(inv.Payload),
		"ruleset_version": ruleset, "approval": approval, "latest_run": run, "latest_fix_task": fixes,
		"open_proposal": open,
		"created_at":    ts(inv.CreatedAt), "updated_at": ts(inv.UpdatedAt),
	})
}

func runResultJSON(res validation.RunResult) map[string]any {
	issues := make([]issueJSON, 0, len(res.Issues))
	for _, i := range res.Issues {
		issues = append(issues, issueFromProto(i))
	}
	return map[string]any{
		"run_id": res.RunID, "payload_version": res.PayloadVersion, "ruleset_version": res.RulesetVersion, "status": res.Status,
		"errors": res.Errors, "warnings": res.Warnings, "fixable_errors": res.FixableErrors, "issues": issues,
	}
}

// writeRunError maps a validation.Service.Run failure to its response.
func writeRunError(w http.ResponseWriter, r *http.Request, err error) {
	var ce *connect.Error
	switch {
	case errors.Is(err, db.ErrNotFound):
		tcError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, validation.ErrStale):
		tcError(w, http.StatusConflict, "stale_payload")
	case errors.Is(err, validation.ErrPermanent):
		tcInternal(r.Context(), w, "validation failed permanently", err)
	case errors.As(err, &ce):
		slog.WarnContext(r.Context(), "trackc: validator unavailable", "err", err)
		tcError(w, http.StatusServiceUnavailable, "validator_unavailable")
	default:
		tcInternal(r.Context(), w, "validation run", err)
	}
}

func (h *trackCHandlers) validateNow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	who := subject(r)
	if !h.inFirm(w, r, "validation requested", func(q *sqlc.Queries, firm uuid.UUID) error {
		inv, err := q.TrackCGetInvoice(ctx, id)
		if err != nil {
			return err
		}
		_, err = audit.Record(ctx, q, firm, audit.Event{
			Actor: audit.Actor{Type: "user", ID: who}, Action: "invoice.validation_requested", EntityType: "invoice", EntityID: id,
			InvoiceID: &id, Before: map[string]any{"status": inv.Status, "payload_version": inv.PayloadVersion},
		})
		return err
	}) {
		return
	}
	res, err := h.m.Validation.Run(ctx, trackc.FirmFrom(ctx), id, validation.RunOpts{Trigger: validation.TriggerManual, Actor: who})
	if err != nil {
		writeRunError(w, r, err)
		return
	}
	trackc.WriteJSON(w, http.StatusOK, runResultJSON(res))
}

func (h *trackCHandlers) listRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var rows []sqlc.TrackCListRunsRow
	if !h.inFirm(w, r, "list runs", func(q *sqlc.Queries, _ uuid.UUID) error {
		if _, err := q.TrackCGetInvoice(ctx, id); err != nil {
			return err
		}
		var err error
		rows, err = q.TrackCListRuns(ctx, sqlc.TrackCListRunsParams{InvoiceID: id, PageLimit: maxRunsListed})
		return err
	}) {
		return
	}
	items := make([]runJSON, 0, len(rows))
	for _, x := range rows {
		items = append(items, runFromListRow(x))
	}
	trackc.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *trackCHandlers) getRun(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	runID, ok := pathID(w, r, "run_id")
	if !ok {
		return
	}
	ctx := r.Context()
	var out runJSON
	if !h.inFirm(w, r, "get run", func(q *sqlc.Queries, _ uuid.UUID) error {
		row, err := q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: runID, InvoiceID: id})
		if err != nil {
			return err
		}
		issues, err := q.TrackCListIssues(ctx, runID)
		if err != nil {
			return err
		}
		out = runFromRow(row)
		is := issuesFromRows(issues)
		out.Issues = &is
		return nil
	}) {
		return
	}
	trackc.WriteJSON(w, http.StatusOK, out)
}

func keySet(rows []sqlc.TrackCListIssuesRow) map[diffKey]bool {
	m := make(map[diffKey]bool, len(rows))
	for _, i := range rows {
		m[diffKey{i.RuleID, i.Path, i.Severity}] = true
	}
	return m
}

func sortedKeys(set, minus map[diffKey]bool) []diffKey {
	out := []diffKey{}
	for k := range set {
		if !minus[k] {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Severity < b.Severity
	})
	return out
}

// runDiff reports the issues that appeared in the run and those that disappeared compared with another
// run of the same invoice (default: the run before it), keyed by (rule_id, path, severity).
func (h *trackCHandlers) runDiff(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	runID, ok := pathID(w, r, "run_id")
	if !ok {
		return
	}
	var explicit uuid.UUID
	if s := r.URL.Query().Get("against"); s != "" {
		var err error
		if explicit, err = uuid.Parse(s); err != nil {
			tcError(w, http.StatusNotFound, "not_found")
			return
		}
	}
	ctx := r.Context()
	var added, removed []diffKey
	var against *uuid.UUID
	if !h.inFirm(w, r, "run diff", func(q *sqlc.Queries, _ uuid.UUID) error {
		run, err := q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: runID, InvoiceID: id})
		if err != nil {
			return err
		}
		var prev uuid.UUID
		if explicit != uuid.Nil {
			p, err := q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: explicit, InvoiceID: id})
			if err != nil {
				return err
			}
			prev = p.ID
		} else {
			rows, err := q.TrackCListRuns(ctx, sqlc.TrackCListRunsParams{InvoiceID: id, PageLimit: 1,
				BeforeCreatedAt: run.CreatedAt, BeforeID: run.ID})
			if err != nil {
				return err
			}
			if len(rows) == 1 {
				prev = rows[0].ID
			}
		}
		cur, err := q.TrackCListIssues(ctx, runID)
		if err != nil {
			return err
		}
		var old []sqlc.TrackCListIssuesRow
		if prev != uuid.Nil {
			against = &prev
			if old, err = q.TrackCListIssues(ctx, prev); err != nil {
				return err
			}
		}
		curSet, oldSet := keySet(cur), keySet(old)
		added, removed = sortedKeys(curSet, oldSet), sortedKeys(oldSet, curSet)
		return nil
	}) {
		return
	}
	trackc.WriteJSON(w, http.StatusOK, map[string]any{"run_id": runID, "against_run_id": against, "added": added, "removed": removed})
}

type correctionsBody struct {
	PayloadVersion int32                   `json:"payload_version"`
	Changes        []fieldpath.FieldChange `json:"changes"`
	Reason         string                  `json:"reason"`
}

func (h *trackCHandlers) corrections(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var b correctionsBody
	if !decodeBody(w, r, &b) {
		return
	}
	if b.PayloadVersion < 1 || len(b.Changes) < 1 || len(b.Changes) > maxChanges || utf8.RuneCountInString(b.Reason) > maxReasonRunes {
		tcError(w, http.StatusBadRequest, "bad_request")
		return
	}
	for _, c := range b.Changes {
		if c.Path == "" {
			tcError(w, http.StatusBadRequest, "bad_request")
			return
		}
	}
	ctx := r.Context()
	firm := trackc.FirmFrom(ctx)
	who := subject(r)
	var newVersion int32
	err := db.WithFirm(ctx, h.m.Pool, firm, func(q *sqlc.Queries) error {
		var err error
		newVersion, err = invoicefix.Apply(ctx, q, firm, id, b.PayloadVersion, b.Changes,
			audit.Actor{Type: "user", ID: who}, false, "invoice.fields_changed", b.Reason)
		return err
	})
	switch {
	case err == nil:
	case errors.Is(err, db.ErrNotFound):
		tcError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, invoicefix.ErrStalePayload):
		tcError(w, http.StatusConflict, "stale_payload")
		return
	case errors.Is(err, fieldpath.ErrBadPath):
		tcError(w, http.StatusBadRequest, "bad_path")
		return
	case errors.Is(err, fieldpath.ErrOldValueMismatch):
		tcError(w, http.StatusConflict, "old_value_mismatch")
		return
	case errors.Is(err, fieldpath.ErrNoChanges):
		tcError(w, http.StatusBadRequest, "bad_request")
		return
	default:
		tcInternal(ctx, w, "apply correction", err)
		return
	}
	// The change is stored; if the validator cannot be reached now the invoice stays "fixed" and the
	// sweeper finishes the re-validation.
	out := map[string]any{"payload_version": newVersion, "revalidation": "done", "run": nil}
	res, rerr := h.m.Validation.Run(ctx, firm, id, validation.RunOpts{Trigger: validation.TriggerCorrection, Actor: who})
	if rerr != nil {
		slog.WarnContext(ctx, "trackc: revalidation after correction pending", "invoice_id", id, "err", rerr)
		out["revalidation"] = "pending"
	} else {
		out["run"] = runResultJSON(res)
	}
	trackc.WriteJSON(w, http.StatusOK, out)
}

type approveBody struct {
	PayloadVersion int32 `json:"payload_version"`
}

func (h *trackCHandlers) approve(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	var b approveBody
	if !decodeBody(w, r, &b) {
		return
	}
	if b.PayloadVersion < 1 {
		tcError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	firm := trackc.FirmFrom(ctx)
	who := subject(r)
	var approved sqlc.TrackCApproveInvoiceRow
	var code string
	err := db.WithFirm(ctx, h.m.Pool, firm, func(q *sqlc.Queries) error {
		cur, err := q.TrackCLockInvoice(ctx, id)
		if err != nil {
			return err
		}
		if cur.PayloadVersion != b.PayloadVersion {
			code = "stale_payload"
			return nil
		}
		if cur.Status != "validated" {
			code = "not_validated"
			return nil
		}
		if approved, err = q.TrackCApproveInvoice(ctx, sqlc.TrackCApproveInvoiceParams{ID: id, PayloadVersion: b.PayloadVersion, ApprovedBy: who}); err != nil {
			return err
		}
		_, err = audit.Record(ctx, q, firm, audit.Event{
			Actor: audit.Actor{Type: "user", ID: who}, Action: "invoice.approved", EntityType: "invoice", EntityID: id, InvoiceID: &id,
			Before: map[string]any{"status": "validated", "payload_version": cur.PayloadVersion},
			After:  map[string]any{"status": "ready", "payload_version": approved.PayloadVersion, "approved_by": who},
		})
		return err
	})
	switch {
	case errors.Is(err, db.ErrNotFound):
		tcError(w, http.StatusNotFound, "not_found")
	case err != nil:
		tcInternal(ctx, w, "approve invoice", err)
	case code != "":
		tcError(w, http.StatusConflict, code)
	default:
		trackc.WriteJSON(w, http.StatusOK, map[string]any{"status": "ready", "payload_version": approved.PayloadVersion,
			"approved_by": who, "approved_at": ts(approved.ApprovedAt)})
	}
}

func (h *trackCHandlers) requestFixes(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	if !h.inFirm(w, r, "check invoice", func(q *sqlc.Queries, _ uuid.UUID) error {
		_, err := q.TrackCGetInvoice(ctx, id)
		return err
	}) {
		return
	}
	if h.m.Fixes == nil { // before gate GB-2 no Fix agent is wired
		tcError(w, http.StatusServiceUnavailable, "fix_unavailable")
		return
	}
	firm := trackc.FirmFrom(ctx)
	who := subject(r)
	task, err := h.m.Fixes.RequestOnDemand(ctx, firm, id, who)
	switch {
	case errors.Is(err, trackc.ErrNoFixableIssues):
		tcError(w, http.StatusConflict, "no_fixable_issues")
		return
	case errors.Is(err, trackc.ErrFixInProgress):
		tcError(w, http.StatusConflict, "fix_in_progress")
		return
	case err != nil:
		tcInternal(ctx, w, "request fix", err)
		return
	}
	if !h.inFirm(w, r, "audit fix request", func(q *sqlc.Queries, _ uuid.UUID) error {
		_, err := audit.Record(ctx, q, firm, audit.Event{
			Actor: audit.Actor{Type: "user", ID: who}, Action: "invoice.fix_requested", EntityType: "invoice", EntityID: id, InvoiceID: &id,
			After: map[string]any{"task_id": task.ID.String(), "mode": task.Mode},
		})
		return err
	}) {
		return
	}
	trackc.WriteJSON(w, http.StatusAccepted, map[string]any{"task_id": task.ID, "mode": task.Mode, "status": task.Status})
}

func (h *trackCHandlers) invoiceAudit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	var rows []sqlc.TrackCListInvoiceAuditRow
	if !h.inFirm(w, r, "invoice audit", func(q *sqlc.Queries, _ uuid.UUID) error {
		if _, err := q.TrackCGetInvoice(ctx, id); err != nil {
			return err
		}
		var err error
		rows, err = q.TrackCListInvoiceAudit(ctx, sqlc.TrackCListInvoiceAuditParams{InvoiceID: id, PageLimit: maxAuditListed})
		return err
	}) {
		return
	}
	type item struct {
		ID         uuid.UUID       `json:"id"`
		OccurredAt time.Time       `json:"occurred_at"`
		ActorType  string          `json:"actor_type"`
		ActorID    string          `json:"actor_id"`
		Agent      string          `json:"agent"`
		ProposalID *uuid.UUID      `json:"proposal_id"`
		Action     string          `json:"action"`
		EntityType string          `json:"entity_type"`
		EntityID   uuid.UUID       `json:"entity_id"`
		Changes    json.RawMessage `json:"changes"`
		Before     json.RawMessage `json:"before"`
		After      json.RawMessage `json:"after"`
		Reason     string          `json:"reason"`
		TraceID    string          `json:"trace_id"`
	}
	items := make([]item, 0, len(rows))
	for _, x := range rows {
		items = append(items, item{ID: x.ID, OccurredAt: ts(x.OccurredAt), ActorType: x.ActorType, ActorID: x.ActorID, Agent: x.Agent,
			ProposalID: optUUID(x.ProposalID), Action: x.Action, EntityType: x.EntityType, EntityID: x.EntityID,
			Changes: rawOrNull(x.Changes), Before: rawOrNull(x.Before), After: rawOrNull(x.After), Reason: x.Reason, TraceID: x.TraceID})
	}
	trackc.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// ---- /v1/exports* ---------------------------------------------------------------------------

func (h *trackCHandlers) createExport(w http.ResponseWriter, r *http.Request) {
	var b struct {
		InvoiceID string `json:"invoice_id"`
	}
	if !decodeBody(w, r, &b) {
		return
	}
	id, err := uuid.Parse(b.InvoiceID)
	if err != nil {
		tcError(w, http.StatusBadRequest, "bad_request")
		return
	}
	ctx := r.Context()
	v, err := h.m.Exports.Create(ctx, trackc.FirmFrom(ctx), id, subject(r))
	var ce *connect.Error
	switch {
	case err == nil:
		trackc.WriteJSON(w, http.StatusCreated, exportJSONOf(v))
	case errors.Is(err, db.ErrNotFound):
		tcError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, exports.ErrNotReady):
		tcError(w, http.StatusConflict, "not_ready")
	case errors.Is(err, exports.ErrRevalidationRequired):
		tcError(w, http.StatusConflict, "revalidation_required")
	case errors.As(err, &ce):
		slog.WarnContext(ctx, "trackc: exporter unavailable", "err", err)
		tcError(w, http.StatusServiceUnavailable, "exporter_unavailable")
	default:
		tcInternal(ctx, w, "create export", err)
	}
}

func (h *trackCHandlers) listExports(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get("invoice_id"))
	if err != nil {
		tcError(w, http.StatusBadRequest, "bad_query")
		return
	}
	ctx := r.Context()
	views, err := h.m.Exports.List(ctx, trackc.FirmFrom(ctx), id)
	if err != nil {
		tcInternal(ctx, w, "list exports", err)
		return
	}
	items := make([]exportJSON, 0, len(views))
	for _, v := range views {
		items = append(items, exportJSONOf(v))
	}
	trackc.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *trackCHandlers) getExport(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	v, err := h.m.Exports.Get(ctx, trackc.FirmFrom(ctx), id)
	switch {
	case err == nil:
		trackc.WriteJSON(w, http.StatusOK, exportJSONOf(v))
	case errors.Is(err, db.ErrNotFound):
		tcError(w, http.StatusNotFound, "not_found")
	default:
		tcInternal(ctx, w, "get export", err)
	}
}

func (h *trackCHandlers) downloadExport(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "id")
	if !ok {
		return
	}
	ctx := r.Context()
	data, v, err := h.m.Exports.Read(ctx, trackc.FirmFrom(ctx), id)
	switch {
	case errors.Is(err, db.ErrNotFound):
		tcError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, exports.ErrCorrupt):
		slog.ErrorContext(ctx, "trackc: export object does not match its digest", "export_id", id, "err", err)
		tcError(w, http.StatusInternalServerError, "export_corrupt")
		return
	case err != nil:
		tcInternal(ctx, w, "read export", err)
		return
	}
	h2 := w.Header()
	h2.Set("Content-Type", exports.ContentType)
	h2.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", v.Filename))
	h2.Set("X-Content-Type-Options", "nosniff")
	h2.Set("Cache-Control", "no-store")
	h2.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
