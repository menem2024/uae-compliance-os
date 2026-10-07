// Package fixtasks asks the Fix agent for proposals and records how each request ended (spec 5.6.8).
//
// api-go only requests and records. The agent (ai-py) proposes through agent.proposal.created, which
// Track B stores in proposals; a person decides in /review. Nothing in this package changes an
// invoice. A request is one fix_tasks row per (validation run, mode) and one agent.task.requested
// message whose Nats-Msg-Id derives from the task id, so a retry never starts the agent twice.
package fixtasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/anypb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// Names fixed by the agent-runtime contract and the spec.
const (
	// Agent is the agent name in AgentTaskRequested and AgentTaskCompleted.
	Agent = "fix"
	// ModeAuto: api-go asked after a validation run. ModeOnDemand: a person pressed "suggest fixes".
	ModeAuto     = "auto"
	ModeOnDemand = "on_demand"
	// SubjectCompleted is the subject ai-py answers on; DurableName filters it on stream AGENTS.
	SubjectCompleted = "agent.task.completed.fix"
	DurableName      = "api-fix-tasks"
	// DLQSubject receives completions that can never be applied.
	DLQSubject = "dlq.agent.fix_results"
	// AutoRequester is requested_by of an automatic request.
	AutoRequester = "system"
)

var (
	// ErrNoFixableIssues: the latest run has no error issue the Fix agent could fix.
	ErrNoFixableIssues = errors.New("fixtasks: no fixable issues")
	// ErrFixInProgress: a task for the invoice's latest run is still running.
	ErrFixInProgress = errors.New("fixtasks: fix in progress")
	// ErrStaleRun: the invoice changed since its latest run; a fix would target an old payload.
	ErrStaleRun = errors.New("fixtasks: latest run is for an older payload")
)

// Budget is the per-task budget of spec 5.6.8. Each call returns a fresh copy.
func Budget() *compliancev1.RunBudget {
	return &compliancev1.RunBudget{MaxLlmCalls: 2, MaxCostMicroUsd: 50000, MaxSteps: 16, MaxInputTokens: 40000,
		MaxOutputTokens: 4000, DeadlineSeconds: 120}
}

// MsgID is the Nats-Msg-Id of a task's agent.task.requested message.
func MsgID(task uuid.UUID) string { return events.AgentTaskRequestedSubject + ":" + task.String() }

// DurableConfig is api-fix-tasks: it sees only the Fix agent's completions and dead-letters to DLQSubject.
func DurableConfig() events.DurableConfig {
	return events.DurableConfig{Stream: events.AgentsStream, Name: DurableName,
		FilterSubjects: []string{SubjectCompleted}, DLQSubject: DLQSubject, MaxDeliver: 5}
}

// Task is a fix_tasks row.
type Task struct {
	ID, InvoiceID, RunID uuid.UUID
	Mode                 string
	Status, Outcome      string
	ProposalID           uuid.UUID // uuid.Nil: none
	AgentRunID           uuid.UUID
	ErrorCode            string
	RequestedBy          string
	RequestedAt          time.Time
	PublishedAt          *time.Time
	CompletedAt          *time.Time
}

func taskOf(r sqlc.TrackCGetFixTaskRow) Task {
	t := Task{ID: r.ID, InvoiceID: r.InvoiceID, RunID: r.RunID, Mode: r.Mode, Status: r.Status, Outcome: r.Outcome,
		ProposalID: r.ProposalID, AgentRunID: r.AgentRunID, ErrorCode: r.ErrorCode, RequestedBy: r.RequestedBy,
		RequestedAt: r.RequestedAt.Time}
	if r.PublishedAt.Valid {
		v := r.PublishedAt.Time
		t.PublishedAt = &v
	}
	if r.CompletedAt.Valid {
		v := r.CompletedAt.Time
		t.CompletedAt = &v
	}
	return t
}

// Requester creates fix tasks and publishes them.
type Requester struct {
	Pool *pgxpool.Pool
	Pub  events.ProtoPublisher
	// Auto: ask the agent after every run that has a fixable error (FIX_AGENT_AUTO, default true).
	Auto bool
}

// OnRun is the validation.Service.OnRun hook: with Auto on, a committed run with at least one fixable
// error requests the Fix agent for that run. It never fails the run: errors are logged, and a task whose
// publish failed is re-published by the Sweeper.
func (r *Requester) OnRun(ctx context.Context, firmID, invoiceID uuid.UUID, res validation.RunResult) {
	if !r.Auto || res.Skipped || res.FixableErrors == 0 {
		return
	}
	_, _, err := r.request(ctx, firmID, invoiceID, res.RunID, ModeAuto, AutoRequester)
	switch {
	case err == nil, errors.Is(err, ErrFixInProgress), errors.Is(err, ErrNoFixableIssues), errors.Is(err, ErrStaleRun):
	default:
		slog.WarnContext(ctx, "fixtasks: automatic fix request failed", "invoice_id", invoiceID, "run_id", res.RunID, "err", err)
	}
}

// RequestOnDemand asks the agent about the invoice's latest run (mode on_demand). It returns
// ErrNoFixableIssues, ErrStaleRun or ErrFixInProgress when there is nothing to ask, the run is out of
// date, or a task is already running. A finished on-demand task for the same run is returned as it is.
func (r *Requester) RequestOnDemand(ctx context.Context, firmID, invoiceID uuid.UUID, requestedBy string) (Task, error) {
	t, created, err := r.request(ctx, firmID, invoiceID, uuid.Nil, ModeOnDemand, requestedBy)
	if err != nil {
		return Task{}, err
	}
	if !created && t.Status == "requested" {
		return Task{}, ErrFixInProgress
	}
	return t, nil
}

// request inserts the task for (run, mode) and publishes it. runID uuid.Nil means the invoice's latest
// run. created reports whether this call inserted the row; when the row already existed it is returned
// unchanged (published again only if its first publish never succeeded).
func (r *Requester) request(ctx context.Context, firmID, invoiceID, runID uuid.UUID, mode, requestedBy string) (Task, bool, error) {
	var (
		task    Task
		snap    snapshot
		created bool
	)
	err := db.WithFirm(ctx, r.Pool, firmID, func(q *sqlc.Queries) error {
		var err error
		if snap, err = loadSnapshot(ctx, q, invoiceID, runID); err != nil {
			return err
		}
		// A task of the other mode still running for this run: do not start a second one.
		if latest, err := q.TrackCLatestFixTask(ctx, invoiceID); err == nil {
			if latest.RunID == snap.run.ID && latest.Status == "requested" && latest.Mode != mode {
				return ErrFixInProgress
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		n, err := q.TrackCInsertFixTask(ctx, sqlc.TrackCInsertFixTaskParams{ID: uuid.New(), FirmID: firmID, InvoiceID: invoiceID,
			RunID: snap.run.ID, Mode: mode, RequestedBy: requestedBy})
		if err != nil {
			return fmt.Errorf("insert fix task: %w", err)
		}
		created = n == 1
		row, err := q.TrackCGetFixTaskByRunMode(ctx, sqlc.TrackCGetFixTaskByRunModeParams{RunID: snap.run.ID, Mode: mode})
		if err != nil {
			return fmt.Errorf("read fix task: %w", err)
		}
		task = taskOf(sqlc.TrackCGetFixTaskRow(row))
		return nil
	})
	if err != nil {
		return Task{}, false, err
	}
	if task.PublishedAt == nil && task.Status == "requested" {
		if perr := r.publish(ctx, firmID, task, snap); perr != nil {
			slog.WarnContext(ctx, "fixtasks: publish failed, the sweeper will retry", "task_id", task.ID, "err", perr)
		} else {
			now := time.Now()
			task.PublishedAt = &now
		}
	}
	return task, created, nil
}

// publish sends agent.task.requested for task and marks it published.
func (r *Requester) publish(ctx context.Context, firmID uuid.UUID, task Task, snap snapshot) error {
	in, err := anypb.New(snap.input)
	if err != nil {
		return fmt.Errorf("pack input: %w", err)
	}
	cc := ""
	if snap.clientCompany != uuid.Nil {
		cc = snap.clientCompany.String()
	}
	msg := &compliancev1.AgentTaskRequested{TaskId: task.ID.String(), FirmId: firmID.String(), ClientCompanyId: cc, Agent: Agent,
		SubjectType: "invoice", SubjectId: task.InvoiceID.String(), Input: in, RequestedBy: task.RequestedBy, Budget: Budget()}
	if err := r.Pub.Publish(ctx, events.AgentTaskRequestedSubject, MsgID(task.ID), msg); err != nil {
		return err
	}
	return db.WithFirm(ctx, r.Pool, firmID, func(q *sqlc.Queries) error {
		_, err := q.TrackCMarkFixTaskPublished(ctx, task.ID)
		return err
	})
}

// republish rebuilds the input of a task whose publish never succeeded and sends it. A task whose
// invoice moved on (or has nothing left to fix) can no longer be sent: it is closed as failed.
func (r *Requester) republish(ctx context.Context, firmID uuid.UUID, task Task) error {
	var snap snapshot
	err := db.WithFirm(ctx, r.Pool, firmID, func(q *sqlc.Queries) error {
		var err error
		snap, err = loadSnapshot(ctx, q, task.InvoiceID, task.RunID)
		return err
	})
	if errors.Is(err, ErrStaleRun) || errors.Is(err, ErrNoFixableIssues) || errors.Is(err, db.ErrNotFound) {
		code := "stale_run"
		if errors.Is(err, ErrNoFixableIssues) {
			code = "no_fixable_issues"
		}
		return db.WithFirm(ctx, r.Pool, firmID, func(q *sqlc.Queries) error {
			_, err := q.TrackCCompleteFixTask(ctx, sqlc.TrackCCompleteFixTaskParams{ID: task.ID, Status: "failed", ErrorCode: code})
			return err
		})
	}
	if err != nil {
		return err
	}
	return r.publish(ctx, firmID, task, snap)
}

// snapshot is what a task's message is built from: the stored payload at the run's version and the
// run's fixable errors.
type snapshot struct {
	run           sqlc.TrackCGetRunRow
	input         *compliancev1.FixTaskInput
	clientCompany uuid.UUID
}

// loadSnapshot reads the invoice, the run (runID, or the invoice's latest when uuid.Nil), its fixable
// error issues and the invoice's client company. q must belong to a db.WithFirm transaction.
func loadSnapshot(ctx context.Context, q *sqlc.Queries, invoiceID, runID uuid.UUID) (snapshot, error) {
	inv, err := q.TrackCGetInvoice(ctx, invoiceID)
	if err != nil {
		return snapshot{}, err
	}
	if runID == uuid.Nil {
		runID = inv.LatestRunID
	}
	if runID == uuid.Nil {
		return snapshot{}, ErrNoFixableIssues
	}
	run, err := q.TrackCGetRun(ctx, sqlc.TrackCGetRunParams{ID: runID, InvoiceID: invoiceID})
	if err != nil {
		return snapshot{}, err
	}
	if run.PayloadVersion != inv.PayloadVersion {
		return snapshot{}, ErrStaleRun
	}
	rows, err := q.TrackCListIssues(ctx, run.ID)
	if err != nil {
		return snapshot{}, err
	}
	issues, err := fixableErrors(rows)
	if err != nil {
		return snapshot{}, err
	}
	if len(issues) == 0 {
		return snapshot{}, ErrNoFixableIssues
	}
	var invoice compliancev1.Invoice
	if err := (protojson.UnmarshalOptions{}).Unmarshal(inv.Payload, &invoice); err != nil {
		return snapshot{}, fmt.Errorf("decode payload of %s: %w", invoiceID, err)
	}
	cc, err := q.TrackCFixInvoiceContext(ctx, invoiceID)
	if err != nil {
		return snapshot{}, err
	}
	return snapshot{run: run, clientCompany: cc, input: &compliancev1.FixTaskInput{InvoiceId: invoiceID.String(),
		PayloadVersion: inv.PayloadVersion, ValidationRunId: run.ID.String(), RulesetVersion: run.RulesetVersion,
		Invoice: &invoice, Issues: issues}}, nil
}

// fixableErrors turns the run's stored issues into protos and keeps the error issues the agent may
// fix (warnings and unfixable errors never reach it).
func fixableErrors(rows []sqlc.TrackCListIssuesRow) ([]*compliancev1.ValidationIssue, error) {
	var out []*compliancev1.ValidationIssue
	for _, r := range rows {
		if r.Severity != "error" || !r.Fixable {
			continue
		}
		args := map[string]string{}
		if len(r.MessageArgs) > 0 {
			if err := json.Unmarshal(r.MessageArgs, &args); err != nil {
				return nil, fmt.Errorf("issue %s: message_args: %w", r.RuleID, err)
			}
		}
		out = append(out, &compliancev1.ValidationIssue{RuleId: r.RuleID, Severity: compliancev1.Severity_SEVERITY_ERROR,
			Path: r.Path, BusinessTerm: r.BusinessTerm, Message: r.Message, MessageAr: r.MessageAr, MessageArgs: args,
			Fixable: true, SuggestedValue: r.SuggestedValue})
	}
	return out, nil
}
