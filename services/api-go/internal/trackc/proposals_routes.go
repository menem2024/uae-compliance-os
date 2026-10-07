package trackc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/auth"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/fixapply"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// Limits of the proposals routes.
const (
	proposalDefaultPage = 25
	proposalMaxPage     = 100
	proposalMaxBody     = 16 << 10
	proposalMaxReason   = 500
)

var (
	proposalStates      = map[string]bool{"proposed": true, "accepted": true, "rejected": true, "superseded": true, "expired": true}
	proposalTargetTypes = map[string]bool{"document": true, "invoice": true, "validation_issue": true, "source": true, "client_company": true}
	proposalKindRE      = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
)

// proposalHandlers serve /v1/proposals* for every proposal kind (PC 6 gives the prefix to Track C).
type proposalHandlers struct {
	m   *Module
	reg *proposals.Registry
}

// proposalMount returns the Mount for the three proposals routes. GETs sit behind the c-read Firm
// stack and the decision behind c-write; every handler reads the Firm only from the context and
// touches tenant rows only through db.WithFirm (inside proposals.List/Decide or here).
func proposalMount(m *Module, reg *proposals.Registry) Mount {
	h := &proposalHandlers{m: m, reg: reg}
	return func(r chi.Router, mw Middlewares) {
		r.With(mw.Read).Get("/v1/proposals", h.list)
		r.With(mw.Read).Get("/v1/proposals/{id}", h.get)
		r.With(mw.Write).Post("/v1/proposals/{id}/decision", h.decide)
	}
}

// proposalView is a proposal as the routes return it: the stored row plus, for an invoice.field_fix,
// its decoded FixProposalDetail (predicted errors before and after, resolved rule ids, per-change notes).
type proposalView struct {
	proposals.Proposal
	DetailJSON json.RawMessage `json:"detail"`
}

func viewOf(p proposals.Proposal) proposalView {
	v := proposalView{Proposal: p, DetailJSON: json.RawMessage("null")}
	if p.Kind != fixapply.KindFieldFix {
		return v
	}
	d, err := fixapply.DecodeDetail(p)
	if err != nil || d == nil {
		return v
	}
	if raw, err := (protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}).Marshal(d); err == nil {
		v.DetailJSON = raw
	}
	return v
}

// OpenInvoiceProposal returns the open invoice.field_fix proposal of an invoice as the routes show it,
// or nil when there is none. q must belong to a db.WithFirm transaction (invoice detail uses it).
func OpenInvoiceProposal(ctx context.Context, q *sqlc.Queries, invoiceID uuid.UUID) (any, error) {
	rows, err := q.ListProposals(ctx, sqlc.ListProposalsParams{
		State:      pgtype.Text{String: proposals.StateProposed, Valid: true},
		Kind:       pgtype.Text{String: fixapply.KindFieldFix, Valid: true},
		TargetType: pgtype.Text{String: "invoice", Valid: true},
		TargetID:   uuid.NullUUID{UUID: invoiceID, Valid: true},
		PageLimit:  1,
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	p, err := proposals.FromRow(rows[0])
	if err != nil {
		return nil, err
	}
	return viewOf(p), nil
}

func proposalCursor(t time.Time, id uuid.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id.String()))
}

func parseProposalCursor(c string) (time.Time, uuid.UUID, error) {
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

func (h *proposalHandlers) list(w http.ResponseWriter, r *http.Request) {
	qv := r.URL.Query()
	bad := func() { WriteError(w, http.StatusBadRequest, "bad_query") }
	f := proposals.ListFilter{State: qv.Get("state"), Kind: qv.Get("kind"), TargetType: qv.Get("target_type")}
	if f.State != "" && !proposalStates[f.State] {
		bad()
		return
	}
	if f.Kind != "" && !proposalKindRE.MatchString(f.Kind) {
		bad()
		return
	}
	if f.TargetType != "" && !proposalTargetTypes[f.TargetType] {
		bad()
		return
	}
	if s := qv.Get("target_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			bad()
			return
		}
		f.TargetID = id
	}
	limit := proposalDefaultPage
	if s := qv.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > proposalMaxPage {
			bad()
			return
		}
		limit = n
	}
	if c := qv.Get("cursor"); c != "" {
		at, id, err := parseProposalCursor(c)
		if err != nil {
			bad()
			return
		}
		f.BeforeCreatedAt, f.BeforeID = at, id
	}
	f.Limit = limit + 1
	rows, err := proposals.List(r.Context(), h.m.Pool, FirmFrom(r.Context()), f)
	if err != nil {
		internal(r.Context(), w, "list proposals", err)
		return
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		c := proposalCursor(rows[limit-1].CreatedAt, rows[limit-1].ID)
		next = &c
	}
	items := make([]proposalView, 0, len(rows))
	for _, p := range rows {
		items = append(items, viewOf(p))
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func (h *proposalHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := proposalID(w, r)
	if !ok {
		return
	}
	var p proposals.Proposal
	err := db.WithFirm(r.Context(), h.m.Pool, FirmFrom(r.Context()), func(q *sqlc.Queries) error {
		row, err := q.TrackCGetProposal(r.Context(), id)
		if err != nil {
			return err
		}
		p, err = proposals.FromRow(row)
		return err
	})
	switch {
	case errors.Is(err, db.ErrNotFound), errors.Is(err, pgx.ErrNoRows):
		WriteError(w, http.StatusNotFound, "not_found")
	case err != nil:
		internal(r.Context(), w, "get proposal", err)
	default:
		WriteJSON(w, http.StatusOK, viewOf(p))
	}
}

func proposalID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusNotFound, "not_found")
		return uuid.Nil, false
	}
	return id, true
}

// decide is E3's entry point: a person accepts or rejects. One accept of an invoice.field_fix writes
// invoice.fields_changed and proposal.accepted in proposals.Decide's transaction; only afterwards does
// it validate the changed invoice (trigger fix_accepted). A validator outage does not undo the accept:
// the response says "revalidation": "pending" and the sweeper finishes the run.
func (h *proposalHandlers) decide(w http.ResponseWriter, r *http.Request) {
	id, ok := proposalID(w, r)
	if !ok {
		return
	}
	var b struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, proposalMaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}
	decision := proposals.Decision(b.Decision)
	if (decision != proposals.Accept && decision != proposals.Reject) || utf8.RuneCountInString(b.Reason) > proposalMaxReason {
		WriteError(w, http.StatusBadRequest, "bad_request")
		return
	}
	p, ok := auth.FromContext(r.Context())
	if !ok || p.Subject == "" {
		WriteError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	ctx := audit.WithActor(r.Context(), audit.Actor{Type: "user", ID: p.Subject})
	firm := FirmFrom(ctx)
	decided, err := proposals.Decide(ctx, h.m.Pool, firm, id, decision, p.Subject, b.Reason, h.reg, fixapply.AuditWriter{})
	switch {
	case err == nil:
	case errors.Is(err, proposals.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found")
		return
	case errors.Is(err, proposals.ErrNotOpen):
		WriteError(w, http.StatusConflict, "proposal_not_open")
		return
	case errors.Is(err, proposals.ErrExpired):
		WriteError(w, http.StatusConflict, "proposal_expired")
		return
	case errors.Is(err, proposals.ErrStale):
		WriteError(w, http.StatusConflict, "stale_proposal")
		return
	case errors.Is(err, proposals.ErrBadProposal):
		slog.WarnContext(ctx, "trackc: proposal cannot be applied", "proposal_id", id, "err", err)
		WriteError(w, http.StatusUnprocessableEntity, "bad_proposal")
		return
	case errors.Is(err, proposals.ErrNoApplier):
		WriteError(w, http.StatusUnprocessableEntity, "no_applier")
		return
	default:
		internal(ctx, w, "decide proposal", err)
		return
	}

	out := map[string]any{"proposal": viewOf(decided), "revalidation": "none", "run": nil}
	if decision == proposals.Accept && decided.Kind == fixapply.KindFieldFix {
		out["revalidation"] = "pending"
		res, err := h.m.Validation.Run(ctx, firm, decided.TargetID, validation.RunOpts{Trigger: validation.TriggerFixAccepted, Actor: p.Subject})
		if err != nil {
			slog.WarnContext(ctx, "trackc: revalidation after accepted fix deferred to the sweeper",
				"proposal_id", id, "invoice_id", decided.TargetID, "err", err)
		} else {
			out["revalidation"] = "done"
			out["run"] = map[string]any{"invoice_id": decided.TargetID, "run_id": res.RunID, "payload_version": res.PayloadVersion,
				"ruleset_version": res.RulesetVersion, "status": res.Status, "errors": res.Errors, "warnings": res.Warnings,
				"fixable_errors": res.FixableErrors}
		}
	}
	WriteJSON(w, http.StatusOK, out)
}

func internal(ctx context.Context, w http.ResponseWriter, what string, err error) {
	slog.ErrorContext(ctx, "trackc: "+what, "err", err)
	WriteError(w, http.StatusInternalServerError, "internal")
}
