package fixtasks

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

// The budget is part of the contract with the agent (spec 5.6.8): one wrong digit is a cost bug.
func TestBudgetIsExactlyTheSpecifiedOne(t *testing.T) {
	b := Budget()
	want := &compliancev1.RunBudget{MaxLlmCalls: 2, MaxCostMicroUsd: 50000, MaxSteps: 16, MaxInputTokens: 40000,
		MaxOutputTokens: 4000, DeadlineSeconds: 120}
	if !proto.Equal(b, want) {
		t.Fatalf("budget = %v, want %v", b, want)
	}
	b.MaxSteps = 1 // callers get their own copy
	if Budget().MaxSteps != 16 {
		t.Fatal("Budget shares state between calls")
	}
}

func TestDurableConfig(t *testing.T) {
	c := DurableConfig()
	if c.Stream != events.AgentsStream || c.Name != "api-fix-tasks" || len(c.FilterSubjects) != 1 ||
		c.FilterSubjects[0] != "agent.task.completed.fix" || c.DLQSubject != "dlq.agent.fix_results" || c.MaxDeliver != 5 {
		t.Fatalf("config = %+v", c)
	}
}

func TestMsgID(t *testing.T) {
	id := uuid.MustParse("6f1c7d50-0c0e-4a4e-9f0b-0f3a1c0a9b11")
	if got := MsgID(id); got != "agent.task.requested:6f1c7d50-0c0e-4a4e-9f0b-0f3a1c0a9b11" {
		t.Fatalf("msg id = %q", got)
	}
}

func TestFixableErrorsOnly(t *testing.T) {
	rows := []sqlc.TrackCListIssuesRow{
		{RuleID: "a", Severity: "error", Path: "currency", Fixable: true, SuggestedValue: "AED", MessageArgs: []byte(`{"expected":"AED"}`)},
		{RuleID: "b", Severity: "warning", Path: "x", Fixable: true},
		{RuleID: "c", Severity: "error", Path: "y", Fixable: false},
		{RuleID: "d", Severity: "error", Path: "z", Fixable: true, MessageAr: "ر", BusinessTerm: "BT-1"},
	}
	got, err := fixableErrors(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].GetRuleId() != "a" || got[1].GetRuleId() != "d" {
		t.Fatalf("issues = %v", got)
	}
	a := got[0]
	if a.GetSeverity() != compliancev1.Severity_SEVERITY_ERROR || !a.GetFixable() || a.GetSuggestedValue() != "AED" ||
		a.GetMessageArgs()["expected"] != "AED" || a.GetPath() != "currency" {
		t.Fatalf("issue = %v", a)
	}
	if got[1].GetMessageAr() != "ر" || got[1].GetBusinessTerm() != "BT-1" {
		t.Fatalf("issue = %v", got[1])
	}
	if _, err := fixableErrors([]sqlc.TrackCListIssuesRow{{RuleID: "e", Severity: "error", Fixable: true, MessageArgs: []byte(`[1]`)}}); err == nil {
		t.Fatal("malformed message_args accepted")
	}
}

func result(t *testing.T, r *compliancev1.FixTaskResult) *anypb.Any {
	t.Helper()
	a, err := anypb.New(r)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestCompletionMapping(t *testing.T) {
	task, firm, run, prop := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	base := func() *compliancev1.AgentTaskCompleted {
		return &compliancev1.AgentTaskCompleted{TaskId: task.String(), FirmId: firm.String(), Agent: "fix", RunId: run.String(),
			Status: compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED,
			Output: result(t, &compliancev1.FixTaskResult{Outcome: "proposed", ProposalId: prop.String(), Changes: 2})}
	}
	t.Run("proposed", func(t *testing.T) {
		c, err := completionOf(base())
		if err != nil {
			t.Fatal(err)
		}
		want := sqlc.TrackCCompleteFixTaskParams{ID: task, Status: "succeeded", Outcome: "proposed", ProposalID: prop, AgentRunID: run}
		if c.firm != firm || c.params != want {
			t.Fatalf("completion = %+v", c)
		}
	})
	t.Run("no fix", func(t *testing.T) {
		m := base()
		m.Output = result(t, &compliancev1.FixTaskResult{Outcome: "no_fix"})
		c, err := completionOf(m)
		if err != nil || c.params.Outcome != "no_fix" || c.params.ProposalID != uuid.Nil {
			t.Fatalf("completion = %+v, %v", c, err)
		}
	})
	t.Run("failed keeps the error code and no outcome", func(t *testing.T) {
		m := base()
		m.Status, m.Output, m.ErrorCode = compliancev1.AgentRunStatus_AGENT_RUN_STATUS_FAILED, nil, "bad_request"
		c, err := completionOf(m)
		if err != nil || c.params.Status != "failed" || c.params.Outcome != "" || c.params.ErrorCode != "bad_request" {
			t.Fatalf("completion = %+v, %v", c, err)
		}
	})
	t.Run("budget exceeded and cancelled", func(t *testing.T) {
		for st, want := range map[compliancev1.AgentRunStatus]string{
			compliancev1.AgentRunStatus_AGENT_RUN_STATUS_BUDGET_EXCEEDED: "budget_exceeded",
			compliancev1.AgentRunStatus_AGENT_RUN_STATUS_CANCELLED:       "cancelled",
		} {
			m := base()
			m.Status, m.Output = st, nil
			c, err := completionOf(m)
			if err != nil || c.params.Status != want {
				t.Fatalf("%v: completion = %+v, %v", st, c, err)
			}
		}
	})
	t.Run("missing agent run id is tolerated", func(t *testing.T) {
		m := base()
		m.RunId = ""
		if c, err := completionOf(m); err != nil || c.params.AgentRunID != uuid.Nil {
			t.Fatalf("completion = %+v, %v", c, err)
		}
	})

	for name, mutate := range map[string]func(*compliancev1.AgentTaskCompleted){
		"bad task id": func(m *compliancev1.AgentTaskCompleted) { m.TaskId = "x" },
		"bad firm id": func(m *compliancev1.AgentTaskCompleted) { m.FirmId = "" },
		"other agent": func(m *compliancev1.AgentTaskCompleted) { m.Agent = "intake" },
		"running": func(m *compliancev1.AgentTaskCompleted) {
			m.Status = compliancev1.AgentRunStatus_AGENT_RUN_STATUS_RUNNING
		},
		"unspecified":       func(m *compliancev1.AgentTaskCompleted) { m.Status = 0 },
		"succeeded no data": func(m *compliancev1.AgentTaskCompleted) { m.Output = nil },
		"unknown outcome": func(m *compliancev1.AgentTaskCompleted) {
			m.Output = result(t, &compliancev1.FixTaskResult{Outcome: "fixed_everything"})
		},
		"bad proposal id": func(m *compliancev1.AgentTaskCompleted) {
			m.Output = result(t, &compliancev1.FixTaskResult{Outcome: "proposed", ProposalId: "p"})
		},
		"foreign output": func(m *compliancev1.AgentTaskCompleted) {
			a, _ := anypb.New(&compliancev1.FixProposalDetail{})
			m.Output = a
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := base()
			mutate(m)
			if _, err := completionOf(m); !errors.Is(err, events.ErrPermanent) {
				t.Fatalf("err = %v, want ErrPermanent", err)
			}
		})
	}
}

// A message that cannot be decoded is dead-lettered at once, without touching the database.
func TestHandleDeadLettersUndecodablePayloads(t *testing.T) {
	c := &Consumer{}
	for name, data := range map[string][]byte{"garbage": {0xff, 0xfe, 0x01}, "wire type clash": {0x0a, 0x05, 'a'}} {
		err := c.Handle(context.Background(), fakeMsg{subject: SubjectCompleted, data: data})
		if !errors.Is(err, events.ErrPermanent) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := c.Handle(context.Background(), fakeMsg{subject: "agent.task.completed.intake", data: nil}); !errors.Is(err, events.ErrPermanent) {
		t.Errorf("other subject: err = %v", err)
	}
}

func TestOnRunSkipsWithoutAutoOrFixableErrors(t *testing.T) {
	// Pool and Pub are nil: reaching either would panic.
	r := &Requester{Auto: false}
	r.OnRun(context.Background(), uuid.New(), uuid.New(), validation.RunResult{FixableErrors: 3, Errors: 3})
	r = &Requester{Auto: true}
	r.OnRun(context.Background(), uuid.New(), uuid.New(), validation.RunResult{FixableErrors: 0, Errors: 2})
	r.OnRun(context.Background(), uuid.New(), uuid.New(), validation.RunResult{FixableErrors: 1, Skipped: true})
}
