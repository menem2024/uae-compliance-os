// Package proposals is the human-in-the-loop store of agent proposals (agent-runtime contract v0.2,
// section 5): insertion from agent.proposal.created, Decide with per-kind Appliers, and listing. Track B
// owns it; Track C mounts the /v1/proposals* routes over Decide and List and registers its own kinds.
package proposals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
)

// Decision is a human's answer to a proposal.
type Decision string

// The two decisions.
const (
	Accept Decision = "accept"
	Reject Decision = "reject"
)

// Proposal states (proposals.state CHECK).
const (
	StateProposed   = "proposed"
	StateAccepted   = "accepted"
	StateRejected   = "rejected"
	StateSuperseded = "superseded"
	StateExpired    = "expired"
)

var (
	ErrNotFound        = errors.New("proposal not found")
	ErrNotOpen         = errors.New("proposal is not open")
	ErrExpired         = errors.New("proposal expired")
	ErrNoApplier       = errors.New("no applier registered for this kind")
	ErrInvalidDecision = errors.New("invalid decision")
	// ErrStale: the target changed since the agent proposed (the proposal stays open).
	ErrStale = errors.New("proposal target changed")
	// ErrBadProposal: the proposal's content cannot be applied by its kind's Applier.
	ErrBadProposal = errors.New("malformed proposal")
)

// Change is one field change ({"path","old_value","new_value"} in proposals.changes).
type Change struct {
	Path     string `json:"path"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

// Evidence is one evidence item ({"kind","ref","excerpt"} in proposals.evidence).
type Evidence struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Excerpt string `json:"excerpt"`
}

// Proposal is one row of the proposals table. Its JSON form is the read-only view /agents shows.
type Proposal struct {
	ID              uuid.UUID         `json:"id"`
	FirmID          uuid.UUID         `json:"-"`
	ClientCompanyID *uuid.UUID        `json:"client_company_id"`
	RunID           *uuid.UUID        `json:"run_id"`
	Agent           string            `json:"agent"`
	Kind            string            `json:"kind"`
	TargetType      string            `json:"target_type"`
	TargetID        uuid.UUID         `json:"target_id"`
	SummaryKey      string            `json:"summary_key"`
	SummaryArgs     map[string]string `json:"summary_args"`
	Rationale       string            `json:"rationale"`
	Confidence      float64           `json:"confidence"`
	Changes         []Change          `json:"changes"`
	DetailType      string            `json:"detail_type"`
	Detail          []byte            `json:"-"`
	Evidence        []Evidence        `json:"evidence"`
	State           string            `json:"state"`
	DecidedBy       string            `json:"decided_by"`
	DecidedAt       *time.Time        `json:"decided_at"`
	DecisionReason  string            `json:"decision_reason"`
	AppliedAt       *time.Time        `json:"applied_at"`
	CreatedAt       time.Time         `json:"created_at"`
	ExpiresAt       *time.Time        `json:"expires_at"`
}

// Change returns the change for path.
func (p Proposal) Change(path string) (Change, bool) {
	for _, c := range p.Changes {
		if c.Path == path {
			return c, true
		}
	}
	return Change{}, false
}

// Applier applies an accepted proposal inside the Decide transaction.
type Applier interface {
	Apply(ctx context.Context, q *sqlc.Queries, p Proposal) error
}

// AuditWriter records the decision (Track C supplies it in Phase 2; nil in Phase 1).
type AuditWriter interface {
	Record(ctx context.Context, q *sqlc.Queries, p Proposal, d Decision, actor, reason string) error
}

// Registry maps a proposal kind to its Applier.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Applier
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{m: map[string]Applier{}} }

// Register adds the Applier for kind; it panics on a duplicate kind (a wiring bug).
func (r *Registry) Register(kind string, a Applier) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.m[kind]; dup {
		panic("proposals: duplicate applier for kind " + kind)
	}
	r.m[kind] = a
}

// Get returns the Applier for kind.
func (r *Registry) Get(kind string) (Applier, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.m[kind]
	return a, ok
}

func ts(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func nullID(n uuid.NullUUID) *uuid.UUID {
	if !n.Valid {
		return nil
	}
	v := n.UUID
	return &v
}

// FromRow maps a sqlc row (JSONB columns decoded).
func FromRow(r sqlc.Proposal) (Proposal, error) {
	p := Proposal{ID: r.ID, FirmID: r.FirmID, ClientCompanyID: nullID(r.ClientCompanyID), RunID: nullID(r.RunID),
		Agent: r.Agent, Kind: r.Kind, TargetType: r.TargetType, TargetID: r.TargetID, SummaryKey: r.SummaryKey,
		Rationale: r.Rationale, DetailType: r.DetailType, Detail: r.Detail, State: r.State,
		DecidedBy: r.DecidedBy.String, DecidedAt: ts(r.DecidedAt), DecisionReason: r.DecisionReason.String,
		AppliedAt: ts(r.AppliedAt), CreatedAt: r.CreatedAt.Time, ExpiresAt: ts(r.ExpiresAt),
		SummaryArgs: map[string]string{}, Changes: []Change{}, Evidence: []Evidence{}}
	if f, err := r.Confidence.Float64Value(); err == nil && f.Valid {
		p.Confidence = f.Float64
	}
	for _, j := range []struct {
		raw []byte
		dst any
	}{{r.SummaryArgs, &p.SummaryArgs}, {r.Changes, &p.Changes}, {r.Evidence, &p.Evidence}} {
		if len(j.raw) == 0 {
			continue
		}
		if err := json.Unmarshal(j.raw, j.dst); err != nil {
			return Proposal{}, fmt.Errorf("decode proposal %s: %w", r.ID, err)
		}
	}
	return p, nil
}

// confidence converts a [0,1] probability to numeric(4,3).
func confidence(v float64) pgtype.Numeric {
	milli := int64(v*1000 + 0.5)
	milli = max(0, min(1000, milli))
	return pgtype.Numeric{Int: big.NewInt(milli), Exp: -3, Valid: true}
}

func parseOptionalID(s string) (uuid.NullUUID, error) {
	if s == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.NullUUID{}, err
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// InsertParams maps an agent.proposal.created payload to its row. A malformed payload is an error the
// consumer dead-letters (it can never succeed).
func InsertParams(p *compliancev1.Proposal, now time.Time) (sqlc.InsertProposalParams, error) {
	id, err := uuid.Parse(p.GetProposalId())
	if err != nil {
		return sqlc.InsertProposalParams{}, fmt.Errorf("proposal_id: %w", err)
	}
	firm, err := uuid.Parse(p.GetFirmId())
	if err != nil {
		return sqlc.InsertProposalParams{}, fmt.Errorf("firm_id: %w", err)
	}
	target, err := uuid.Parse(p.GetTargetId())
	if err != nil {
		return sqlc.InsertProposalParams{}, fmt.Errorf("target_id: %w", err)
	}
	cc, err := parseOptionalID(p.GetClientCompanyId())
	if err != nil {
		return sqlc.InsertProposalParams{}, fmt.Errorf("client_company_id: %w", err)
	}
	run, err := parseOptionalID(p.GetRunId())
	if err != nil {
		return sqlc.InsertProposalParams{}, fmt.Errorf("run_id: %w", err)
	}
	changes := make([]Change, 0, len(p.GetChanges()))
	for _, c := range p.GetChanges() {
		changes = append(changes, Change{Path: c.GetPath(), OldValue: c.GetOldValue(), NewValue: c.GetNewValue()})
	}
	evidence := make([]Evidence, 0, len(p.GetEvidence()))
	for _, e := range p.GetEvidence() {
		evidence = append(evidence, Evidence{Kind: e.GetKind(), Ref: e.GetRef(), Excerpt: e.GetExcerpt()})
	}
	args := p.GetSummaryArgs()
	if args == nil {
		args = map[string]string{}
	}
	argsJSON, _ := json.Marshal(args)
	changesJSON, _ := json.Marshal(changes)
	evidenceJSON, _ := json.Marshal(evidence)
	created := now
	if p.GetCreatedAt().IsValid() {
		created = p.GetCreatedAt().AsTime()
	}
	out := sqlc.InsertProposalParams{ID: id, FirmID: firm, ClientCompanyID: cc, RunID: run, Agent: p.GetAgent(),
		Kind: p.GetKind(), TargetType: p.GetTargetType(), TargetID: target, SummaryKey: p.GetSummaryKey(),
		SummaryArgs: argsJSON, Rationale: p.GetRationale(), Confidence: confidence(p.GetConfidence()),
		Changes: changesJSON, Evidence: evidenceJSON, CreatedAt: pgtype.Timestamptz{Time: created, Valid: true}}
	if d := p.GetDetail(); d != nil {
		out.DetailType, out.Detail = d.GetTypeUrl(), d.GetValue()
	}
	if p.GetExpiresAt().IsValid() {
		out.ExpiresAt = pgtype.Timestamptz{Time: p.GetExpiresAt().AsTime(), Valid: true}
	}
	return out, nil
}

// Insert supersedes any other open proposal for the same (target, kind) and inserts this one, in the
// caller's transaction (contract section 5 step 2). A redelivered proposal changes nothing.
func Insert(ctx context.Context, q *sqlc.Queries, arg sqlc.InsertProposalParams) (bool, error) {
	if _, err := q.SupersedeOpenProposals(ctx, sqlc.SupersedeOpenProposalsParams{TargetType: arg.TargetType,
		TargetID: arg.TargetID, Kind: arg.Kind, NewID: arg.ID}); err != nil {
		return false, fmt.Errorf("supersede proposals: %w", err)
	}
	n, err := q.InsertProposal(ctx, arg)
	if err != nil {
		return false, fmt.Errorf("insert proposal: %w", err)
	}
	return n == 1, nil
}

// Decide applies a human decision in one db.WithFirm transaction: lock, check open and not expired,
// run the kind's Applier (accept only), set the state, record the audit. An Applier error rolls
// everything back and the proposal stays proposed. An expired proposal is marked expired (committed)
// and ErrExpired is returned.
func Decide(ctx context.Context, pool *pgxpool.Pool, firmID, id uuid.UUID, d Decision, actor, reason string,
	reg *Registry, audit AuditWriter) (Proposal, error) {
	if (d != Accept && d != Reject) || actor == "" {
		return Proposal{}, ErrInvalidDecision
	}
	var out Proposal
	expired := false
	err := db.WithFirm(ctx, pool, firmID, func(q *sqlc.Queries) error {
		row, err := q.LockProposal(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lock proposal: %w", err)
		}
		p, err := FromRow(row)
		if err != nil {
			return err
		}
		if p.State != StateProposed {
			return ErrNotOpen
		}
		if p.ExpiresAt != nil && !p.ExpiresAt.After(time.Now()) {
			if _, err := q.ExpireProposal(ctx, id); err != nil {
				return fmt.Errorf("expire proposal: %w", err)
			}
			p.State = StateExpired
			out, expired = p, true
			return nil
		}
		params := sqlc.DecideProposalParams{ID: id, State: StateRejected,
			DecidedBy:      pgtype.Text{String: actor, Valid: true},
			DecisionReason: pgtype.Text{String: reason, Valid: reason != ""}}
		if d == Accept {
			a, ok := reg.Get(p.Kind)
			if !ok {
				return ErrNoApplier
			}
			if err := a.Apply(ctx, q, p); err != nil {
				return err
			}
			params.State = StateAccepted
			params.AppliedAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
		}
		row, err = q.DecideProposal(ctx, params)
		if err != nil {
			return fmt.Errorf("decide proposal: %w", err)
		}
		if out, err = FromRow(row); err != nil {
			return err
		}
		if audit != nil {
			return audit.Record(ctx, q, out, d, actor, reason)
		}
		return nil
	})
	if errors.Is(err, db.ErrNotFound) {
		return Proposal{}, ErrNotFound
	}
	if err != nil {
		return Proposal{}, err
	}
	if expired {
		return out, ErrExpired
	}
	return out, nil
}

// ListFilter selects proposals (newest first); zero values mean "any".
type ListFilter struct {
	State, Kind, TargetType string
	TargetID                uuid.UUID
	BeforeCreatedAt         time.Time
	BeforeID                uuid.UUID
	Limit                   int
}

// List returns one page of the Firm's proposals.
func List(ctx context.Context, pool *pgxpool.Pool, firmID uuid.UUID, f ListFilter) ([]Proposal, error) {
	var out []Proposal
	err := db.WithFirm(ctx, pool, firmID, func(q *sqlc.Queries) error {
		p := sqlc.ListProposalsParams{PageLimit: int32(max(1, min(f.Limit, 101)))} //nolint:gosec // bounded
		text := func(s string) pgtype.Text { return pgtype.Text{String: s, Valid: s != ""} }
		p.State, p.Kind, p.TargetType = text(f.State), text(f.Kind), text(f.TargetType)
		if f.TargetID != uuid.Nil {
			p.TargetID = uuid.NullUUID{UUID: f.TargetID, Valid: true}
		}
		if f.BeforeID != uuid.Nil {
			p.BeforeCreatedAt = pgtype.Timestamptz{Time: f.BeforeCreatedAt, Valid: true}
			p.BeforeID = uuid.NullUUID{UUID: f.BeforeID, Valid: true}
		}
		rows, err := q.ListProposals(ctx, p)
		if err != nil {
			return fmt.Errorf("list proposals: %w", err)
		}
		return appendRows(&out, rows)
	})
	return out, err
}

// ForRun returns the proposals of one run (the /agents run drawer).
func ForRun(ctx context.Context, q *sqlc.Queries, runID uuid.UUID) ([]Proposal, error) {
	out := []Proposal{}
	rows, err := q.ListRunProposals(ctx, uuid.NullUUID{UUID: runID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list run proposals: %w", err)
	}
	if err := appendRows(&out, rows); err != nil {
		return nil, err
	}
	return out, nil
}

func appendRows(out *[]Proposal, rows []sqlc.Proposal) error {
	for _, r := range rows {
		p, err := FromRow(r)
		if err != nil {
			return err
		}
		*out = append(*out, p)
	}
	return nil
}

// IsPermanentPG reports a Postgres error no retry can fix (integrity or data exception classes).
func IsPermanentPG(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && len(pg.Code) == 5 && (pg.Code[:2] == "23" || pg.Code[:2] == "22")
}
