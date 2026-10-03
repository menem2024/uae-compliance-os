// Package agents persists and serves the agent orchestra's activity: the api-agent-events consumer
// (this file), the live SSE feed and the /v1/agents routes (Task 19).
package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// EventsDurableConfig is the api-agent-events durable (contract section 4: it never sees task traffic).
func EventsDurableConfig() events.DurableConfig {
	return events.DurableConfig{
		Stream:         events.AgentsStream,
		Name:           events.AgentEventsDurable,
		FilterSubjects: []string{"agent.run.>", "agent.proposal.>"},
		DLQSubject:     events.DLQAgentEventsSubject,
		Workers:        8,
	}
}

// Consumer writes agent events to agent_runs, agent_steps and proposals. Every write is idempotent and
// order-independent (Task 3 queries), so parallel workers and redeliveries are safe.
type Consumer struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

func (c *Consumer) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func permanent(format string, args ...any) error {
	return fmt.Errorf("%w: %s", events.ErrPermanent, fmt.Sprintf(format, args...))
}

// Handle is the events.MsgHandler.
func (c *Consumer) Handle(ctx context.Context, msg jetstream.Msg) error {
	var err error
	switch msg.Subject() {
	case events.AgentRunStartedSubject:
		m := &compliancev1.AgentRunStarted{}
		if err = unmarshal(msg.Data(), m); err == nil {
			err = c.runStarted(ctx, m)
		}
	case events.AgentRunStepSubject:
		m := &compliancev1.AgentStepEvent{}
		if err = unmarshal(msg.Data(), m); err == nil {
			err = c.step(ctx, m)
		}
	case events.AgentRunFinishedSubject:
		m := &compliancev1.AgentRunFinished{}
		if err = unmarshal(msg.Data(), m); err == nil {
			err = c.runFinished(ctx, m)
		}
	case events.AgentProposalCreatedSubject:
		m := &compliancev1.ProposalCreated{}
		if err = unmarshal(msg.Data(), m); err == nil {
			err = c.proposal(ctx, m)
		}
	default:
		return permanent("unexpected subject %s", msg.Subject())
	}
	if err != nil && !errors.Is(err, events.ErrPermanent) && proposals.IsPermanentPG(err) {
		slog.WarnContext(ctx, "agents consumer: dead-lettering a result the database rejects for good",
			"subject", msg.Subject(), "err", err)
		return fmt.Errorf("%w: %w", events.ErrPermanent, err)
	}
	return err
}

func unmarshal(data []byte, m proto.Message) error {
	if err := proto.Unmarshal(data, m); err != nil {
		return permanent("decode %T: %v", m, err)
	}
	return nil
}

func (c *Consumer) withFirm(ctx context.Context, firm string, fn func(q *sqlc.Queries) error) error {
	firmID, err := uuid.Parse(firm)
	if err != nil {
		return permanent("firm_id %q", firm)
	}
	return db.WithFirm(ctx, c.Pool, firmID, fn)
}

func (c *Consumer) runStarted(ctx context.Context, m *compliancev1.AgentRunStarted) error {
	p, err := RunStartedParams(m, c.now())
	if err != nil {
		return err
	}
	return c.withFirm(ctx, m.GetFirmId(), func(q *sqlc.Queries) error {
		if err := q.UpsertRunStarted(ctx, p); err != nil {
			return fmt.Errorf("upsert run started: %w", err)
		}
		if _, err := q.AbandonSupersededRuns(ctx, sqlc.AbandonSupersededRunsParams{SubjectType: p.SubjectType,
			SubjectID: p.SubjectID}); err != nil {
			return fmt.Errorf("abandon superseded runs: %w", err)
		}
		if docID, err := uuid.Parse(p.SubjectID); err == nil && p.SubjectType == "document" {
			n, err := q.MarkDocumentProcessing(ctx, sqlc.MarkDocumentProcessingParams{ID: docID,
				RunID: uuid.NullUUID{UUID: p.ID, Valid: true}})
			if err != nil {
				return fmt.Errorf("mark document processing: %w", err)
			}
			if n == 0 {
				// A terminal Document is the normal reason. A Document this Firm cannot see is not: NATS
				// carries no auth, so firm_id is a claim, and RLS made the write a no-op. Ack, but say so.
				if _, err := q.GetDocument(ctx, docID); errors.Is(err, pgx.ErrNoRows) {
					slog.WarnContext(ctx, "agents consumer: document not owned by the message's firm, ignored",
						"subject", events.AgentRunStartedSubject, "firm_id", m.GetFirmId(), "document_id", docID)
				} else if err != nil {
					return fmt.Errorf("get document: %w", err)
				}
			}
		}
		return nil
	})
}

func (c *Consumer) step(ctx context.Context, m *compliancev1.AgentStepEvent) error {
	p, err := StepParams(m, c.now())
	if err != nil {
		return err
	}
	return c.withFirm(ctx, m.GetFirmId(), func(q *sqlc.Queries) error {
		if err := q.UpsertStep(ctx, p); err != nil {
			return fmt.Errorf("upsert step: %w", err)
		}
		return nil
	})
}

func (c *Consumer) runFinished(ctx context.Context, m *compliancev1.AgentRunFinished) error {
	p, err := RunFinishedParams(m, c.now())
	if err != nil {
		return err
	}
	return c.withFirm(ctx, m.GetFirmId(), func(q *sqlc.Queries) error {
		if err := q.UpsertRunFinished(ctx, p); err != nil {
			return fmt.Errorf("upsert run finished: %w", err)
		}
		return nil
	})
}

func (c *Consumer) proposal(ctx context.Context, m *compliancev1.ProposalCreated) error {
	if m.GetProposal() == nil {
		return permanent("empty proposal")
	}
	p, err := proposals.InsertParams(m.GetProposal(), c.now())
	if err != nil {
		return permanent("proposal: %v", err)
	}
	return c.withFirm(ctx, m.GetProposal().GetFirmId(), func(q *sqlc.Queries) error {
		_, err := proposals.Insert(ctx, q, p)
		return err
	})
}

// ------------------------------------------------------------------ proto -> row mapping (pure)

// EnumName lower-cases a proto enum value name without its prefix (AGENT_STEP_STATUS_SUCCEEDED ->
// "succeeded"); UNSPECIFIED maps to "".
func EnumName(full, prefix string) string {
	name := strings.ToLower(strings.TrimPrefix(full, prefix))
	if name == "unspecified" {
		return ""
	}
	return name
}

// RunStatus maps AgentRunStatus to agent_runs.status.
func RunStatus(s compliancev1.AgentRunStatus) string {
	return EnumName(s.String(), "AGENT_RUN_STATUS_")
}

// StepStatus maps AgentStepStatus to agent_steps.status.
func StepStatus(s compliancev1.AgentStepStatus) string {
	return EnumName(s.String(), "AGENT_STEP_STATUS_")
}

// Kind maps StepKind to agent_steps.kind.
func Kind(k compliancev1.StepKind) string { return EnumName(k.String(), "STEP_KIND_") }

func tsOr(t *timestamppb.Timestamp, def time.Time) pgtype.Timestamptz {
	if t.IsValid() {
		return pgtype.Timestamptz{Time: t.AsTime(), Valid: true}
	}
	return pgtype.Timestamptz{Time: def, Valid: true}
}

func optionalID(s string) (uuid.NullUUID, error) {
	if s == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return uuid.NullUUID{}, permanent("id %q", s)
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

// PlanNode is one node of agent_runs.plan (the static DAG /agents draws before anything runs).
type PlanNode struct {
	NodeID    string   `json:"node_id"`
	Agent     string   `json:"agent"`
	Action    string   `json:"action"`
	Kind      string   `json:"kind"`
	DependsOn []string `json:"depends_on"`
}

// RunBudget is agent_runs.budget.
type RunBudget struct {
	MaxSteps        int32 `json:"max_steps"`
	MaxLLMCalls     int32 `json:"max_llm_calls"`
	MaxInputTokens  int64 `json:"max_input_tokens"`
	MaxOutputTokens int64 `json:"max_output_tokens"`
	MaxCostMicroUSD int64 `json:"max_cost_micro_usd"`
	DeadlineSeconds int32 `json:"deadline_seconds"`
}

// RunStartedParams maps agent.run.started.
func RunStartedParams(m *compliancev1.AgentRunStarted, now time.Time) (sqlc.UpsertRunStartedParams, error) {
	id, err := uuid.Parse(m.GetRunId())
	if err != nil {
		return sqlc.UpsertRunStartedParams{}, permanent("run_id %q", m.GetRunId())
	}
	firm, err := uuid.Parse(m.GetFirmId())
	if err != nil {
		return sqlc.UpsertRunStartedParams{}, permanent("firm_id %q", m.GetFirmId())
	}
	cc, err := optionalID(m.GetClientCompanyId())
	if err != nil {
		return sqlc.UpsertRunStartedParams{}, err
	}
	if m.GetSubjectType() == "" || m.GetSubjectId() == "" {
		return sqlc.UpsertRunStartedParams{}, permanent("run %s has no subject", id)
	}
	plan := make([]PlanNode, 0, len(m.GetPlan()))
	for _, n := range m.GetPlan() {
		deps := n.GetDependsOn()
		if deps == nil {
			deps = []string{}
		}
		plan = append(plan, PlanNode{NodeID: n.GetNodeId(), Agent: n.GetAgent(), Action: n.GetAction(),
			Kind: Kind(n.GetKind()), DependsOn: deps})
	}
	b := m.GetBudget()
	planJSON, _ := json.Marshal(plan)
	budgetJSON, _ := json.Marshal(RunBudget{MaxSteps: b.GetMaxSteps(), MaxLLMCalls: b.GetMaxLlmCalls(),
		MaxInputTokens: b.GetMaxInputTokens(), MaxOutputTokens: b.GetMaxOutputTokens(),
		MaxCostMicroUSD: b.GetMaxCostMicroUsd(), DeadlineSeconds: b.GetDeadlineSeconds()})
	return sqlc.UpsertRunStartedParams{ID: id, FirmID: firm, ClientCompanyID: cc, Workflow: m.GetWorkflow(),
		SubjectType: m.GetSubjectType(), SubjectID: m.GetSubjectId(),
		DeliveryAttempt: max(1, m.GetDeliveryAttempt()), TraceID: m.GetTraceId(), Plan: planJSON,
		Budget: budgetJSON, StartedAt: tsOr(m.GetStartedAt(), now)}, nil
}

// StepParams maps agent.run.step.
func StepParams(m *compliancev1.AgentStepEvent, now time.Time) (sqlc.UpsertStepParams, error) {
	id, err := uuid.Parse(m.GetStepId())
	if err != nil {
		return sqlc.UpsertStepParams{}, permanent("step_id %q", m.GetStepId())
	}
	run, err := uuid.Parse(m.GetRunId())
	if err != nil {
		return sqlc.UpsertStepParams{}, permanent("run_id %q", m.GetRunId())
	}
	firm, err := uuid.Parse(m.GetFirmId())
	if err != nil {
		return sqlc.UpsertStepParams{}, permanent("firm_id %q", m.GetFirmId())
	}
	cc, err := optionalID(m.GetClientCompanyId())
	if err != nil {
		return sqlc.UpsertStepParams{}, err
	}
	status, kind := StepStatus(m.GetStatus()), Kind(m.GetKind())
	if status == "" || kind == "" || m.GetSeq() < 1 {
		return sqlc.UpsertStepParams{}, permanent("step %s: status %q kind %q seq %d", id, status, kind, m.GetSeq())
	}
	args := m.GetMessageArgs()
	if args == nil {
		args = map[string]string{}
	}
	argsJSON, _ := json.Marshal(args)
	deps := m.GetDependsOn()
	if deps == nil {
		deps = []string{}
	}
	u := m.GetUsage()
	return sqlc.UpsertStepParams{ID: id, FirmID: firm, RunID: run, Seq: m.GetSeq(), NodeID: m.GetNodeId(),
		DependsOn: deps, Agent: m.GetAgent(), Action: m.GetAction(), Kind: kind, Status: status,
		Attempt: m.GetAttempt(), At: tsOr(m.GetAt(), now), DurationMs: m.GetDurationMs(), Model: u.GetModel(),
		PromptID: u.GetPromptId(), PromptVersion: u.GetPromptVersion(), InputTokens: u.GetInputTokens(),
		OutputTokens: u.GetOutputTokens(), CacheReadInputTokens: u.GetCacheReadInputTokens(),
		CacheCreationInputTokens: u.GetCacheCreationInputTokens(), CostMicroUsd: max(0, u.GetCostMicroUsd()),
		ResponseCacheHit: u.GetResponseCacheHit(), LlmCalls: u.GetLlmCalls(), MessageKey: m.GetMessageKey(),
		MessageArgs: argsJSON, ErrorCode: m.GetErrorCode(), SubjectType: m.GetSubjectType(),
		SubjectID: m.GetSubjectId(), ClientCompanyID: cc}, nil
}

// RunFinishedParams maps agent.run.finished.
func RunFinishedParams(m *compliancev1.AgentRunFinished, now time.Time) (sqlc.UpsertRunFinishedParams, error) {
	id, err := uuid.Parse(m.GetRunId())
	if err != nil {
		return sqlc.UpsertRunFinishedParams{}, permanent("run_id %q", m.GetRunId())
	}
	firm, err := uuid.Parse(m.GetFirmId())
	if err != nil {
		return sqlc.UpsertRunFinishedParams{}, permanent("firm_id %q", m.GetFirmId())
	}
	status := RunStatus(m.GetStatus())
	if status == "" || status == "running" || m.GetSubjectType() == "" || m.GetSubjectId() == "" {
		return sqlc.UpsertRunFinishedParams{}, permanent("run %s: bad final status %q or subject", id, status)
	}
	t := m.GetTotals()
	return sqlc.UpsertRunFinishedParams{ID: id, FirmID: firm, SubjectType: m.GetSubjectType(),
		SubjectID: m.GetSubjectId(), Status: status, ErrorCode: m.GetErrorCode(), Steps: t.GetSteps(),
		LlmCalls: t.GetLlmCalls(), ResponseCacheHits: t.GetResponseCacheHits(), InputTokens: t.GetInputTokens(),
		OutputTokens: t.GetOutputTokens(), CostMicroUsd: max(0, t.GetCostMicroUsd()),
		FinishedAt: tsOr(m.GetFinishedAt(), now)}, nil
}
