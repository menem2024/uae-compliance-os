package agents_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/agents"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

// fakeMsg implements the two jetstream.Msg methods Handle uses; any other call panics.
type fakeMsg struct {
	jetstream.Msg
	subject string
	data    []byte
}

func (m fakeMsg) Subject() string { return m.subject }
func (m fakeMsg) Data() []byte    { return m.data }

var now = time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)

func TestEnumNames(t *testing.T) {
	if agents.RunStatus(compliancev1.AgentRunStatus_AGENT_RUN_STATUS_BUDGET_EXCEEDED) != "budget_exceeded" ||
		agents.StepStatus(compliancev1.AgentStepStatus_AGENT_STEP_STATUS_RETRYING) != "retrying" ||
		agents.Kind(compliancev1.StepKind_STEP_KIND_ROUTER) != "router" ||
		agents.RunStatus(compliancev1.AgentRunStatus_AGENT_RUN_STATUS_UNSPECIFIED) != "" {
		t.Fatal("enum mapping")
	}
	cfg := agents.EventsDurableConfig()
	if cfg.Name != "api-agent-events" || cfg.Stream != "AGENTS" || len(cfg.FilterSubjects) != 2 ||
		cfg.FilterSubjects[0] != "agent.run.>" || cfg.DLQSubject != "dlq.agent.events" {
		t.Fatalf("%+v", cfg)
	}
}

func TestRunStartedParams(t *testing.T) {
	m := &compliancev1.AgentRunStarted{RunId: uuid.NewString(), FirmId: uuid.NewString(), Workflow: "document_ingestion@1",
		SubjectType: "document", SubjectId: uuid.NewString(), DeliveryAttempt: 2, TraceId: "abc",
		Budget:    &compliancev1.RunBudget{MaxSteps: 40, MaxLlmCalls: 6, MaxCostMicroUsd: 150000, DeadlineSeconds: 300},
		StartedAt: timestamppb.New(now),
		Plan: []*compliancev1.GraphNode{{NodeId: "fetch", Agent: "orchestrator", Action: "fetch",
			Kind: compliancev1.StepKind_STEP_KIND_DETERMINISTIC}, {NodeId: "route", Agent: "orchestrator", Action: "route",
			Kind: compliancev1.StepKind_STEP_KIND_ROUTER, DependsOn: []string{"fetch"}}}}
	p, err := agents.RunStartedParams(m, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var plan []agents.PlanNode
	var budget agents.RunBudget
	if err := json.Unmarshal(p.Plan, &plan); err != nil || json.Unmarshal(p.Budget, &budget) != nil {
		t.Fatal(err)
	}
	if len(plan) != 2 || plan[0].Kind != "deterministic" || plan[0].DependsOn == nil || plan[1].DependsOn[0] != "fetch" ||
		budget.MaxLLMCalls != 6 || budget.DeadlineSeconds != 300 || p.ClientCompanyID.Valid || p.DeliveryAttempt != 2 ||
		!p.StartedAt.Time.Equal(now) {
		t.Fatalf("%+v %+v %+v", p, plan, budget)
	}
	m.SubjectId = ""
	if _, err := agents.RunStartedParams(m, now); !errors.Is(err, events.ErrPermanent) {
		t.Fatalf("no subject: %v", err)
	}
}

func TestStepAndFinishedParams(t *testing.T) {
	step := &compliancev1.AgentStepEvent{RunId: uuid.NewString(), FirmId: uuid.NewString(), StepId: uuid.NewString(),
		Seq: 7, NodeId: "extraction.extract", Agent: "extraction", Action: "extract", Kind: compliancev1.StepKind_STEP_KIND_LLM,
		Status: compliancev1.AgentStepStatus_AGENT_STEP_STATUS_SUCCEEDED, Attempt: 1, DurationMs: 900,
		Usage: &compliancev1.ModelUsage{Model: "claude-sonnet-5", PromptId: "extraction.invoice", PromptVersion: 1,
			InputTokens: 4500, OutputTokens: 800, CostMicroUsd: 17000, LlmCalls: 1},
		MessageKey: "extraction.extracted", MessageArgs: map[string]string{"fields": "31"}, SubjectType: "document",
		SubjectId: uuid.NewString(), ClientCompanyId: uuid.NewString()}
	p, err := agents.StepParams(step, now)
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != "llm" || p.Status != "succeeded" || p.LlmCalls != 1 || p.CostMicroUsd != 17000 || p.Model != "claude-sonnet-5" ||
		string(p.MessageArgs) != `{"fields":"31"}` || !p.ClientCompanyID.Valid || !p.At.Time.Equal(now) || p.DependsOn == nil {
		t.Fatalf("%+v", p)
	}
	step.Status = compliancev1.AgentStepStatus_AGENT_STEP_STATUS_UNSPECIFIED
	if _, err := agents.StepParams(step, now); !errors.Is(err, events.ErrPermanent) {
		t.Fatalf("bad status: %v", err)
	}
	fin := &compliancev1.AgentRunFinished{RunId: uuid.NewString(), FirmId: uuid.NewString(),
		Status: compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED, SubjectType: "document", SubjectId: "d",
		Totals: &compliancev1.RunTotals{Steps: 12, LlmCalls: 2, ResponseCacheHits: 1, CostMicroUsd: 21000}}
	f, err := agents.RunFinishedParams(fin, now)
	if err != nil || f.Status != "succeeded" || f.Steps != 12 || f.ResponseCacheHits != 1 || !f.FinishedAt.Time.Equal(now) {
		t.Fatalf("%+v %v", f, err)
	}
	fin.Status = compliancev1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING
	if _, err := agents.RunFinishedParams(fin, now); !errors.Is(err, events.ErrPermanent) {
		t.Fatal("running is not a final status")
	}
}

func TestHandleDeadLettersWhatCanNeverSucceed(t *testing.T) {
	c := &agents.Consumer{Now: func() time.Time { return now }}
	ctx := context.Background()
	bad, _ := proto.Marshal(&compliancev1.AgentRunStarted{RunId: "nope", FirmId: uuid.NewString()})
	for _, m := range []fakeMsg{
		{subject: "agent.task.requested", data: nil},
		{subject: events.AgentRunStepSubject, data: []byte{0xff, 0xff, 0xff}},
		{subject: events.AgentRunStartedSubject, data: bad},
		{subject: events.AgentProposalCreatedSubject, data: nil},
	} {
		if err := c.Handle(ctx, m); !errors.Is(err, events.ErrPermanent) {
			t.Errorf("%s: %v", m.subject, err)
		}
	}
}
