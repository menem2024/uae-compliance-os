//go:build integration

// The Fix agent wiring of the Track C module: a validation run with a fixable error requests the agent
// (FIX_AGENT_AUTO), the on-demand route asks again, and the api-fix-tasks consumer records the answer.
// The agent is a stand-in (the test publishes AgentTaskCompleted); an embedded JetStream server carries
// the messages and no model is involved.
package trackc_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/anypb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

type fixDetail struct {
	LatestFixTask *struct {
		ID         uuid.UUID  `json:"id"`
		Mode       string     `json:"mode"`
		Status     string     `json:"status"`
		Outcome    string     `json:"outcome"`
		ProposalID *uuid.UUID `json:"proposal_id"`
	} `json:"latest_fix_task"`
}

func (s *propStack) fixTask(t *testing.T, id uuid.UUID) *struct {
	ID         uuid.UUID  `json:"id"`
	Mode       string     `json:"mode"`
	Status     string     `json:"status"`
	Outcome    string     `json:"outcome"`
	ProposalID *uuid.UUID `json:"proposal_id"`
} {
	t.Helper()
	var d fixDetail
	s.must(t, "org-a", "u", http.MethodGet, "/v1/invoices/"+id.String()+"/validation", nil, http.StatusOK, &d)
	return d.LatestFixTask
}

func TestFixAgentWiring(t *testing.T) {
	js := eventstest.StartJetStream(t)
	s := newPropStackJS(t, js.JS)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.tc.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(10 * time.Second)
	for s.tc.Ready(ctx) != nil {
		if time.Now().After(deadline) {
			t.Fatal("api-fix-tasks never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// A run with a fixable error requests the agent by itself.
	inv := s.env.SeedInvoice(t, s.env.FirmA, propPayload)
	if _, err := s.svc.Run(ctx, s.env.FirmA, inv, validation.RunOpts{Trigger: validation.TriggerExtracted}); err != nil {
		t.Fatal(err)
	}
	task := s.fixTask(t, inv)
	if task == nil || task.Mode != "auto" || task.Status != "requested" {
		t.Fatalf("latest fix task = %+v", task)
	}
	// While it runs, asking again is refused.
	code, raw := s.do(t, "org-a", "alice", http.MethodPost, "/v1/invoices/"+inv.String()+"/validation/fixes", nil)
	if code != http.StatusConflict || !strings.Contains(string(raw), "fix_in_progress") {
		t.Fatalf("on-demand while auto runs = %d %s", code, raw)
	}

	// The agent answers; the consumer records it.
	proposal := uuid.New()
	answer := func(task uuid.UUID, res *compliancev1.FixTaskResult) {
		out, err := anypb.New(res)
		if err != nil {
			t.Fatal(err)
		}
		err = events.NewBus(js.JS).Publish(ctx, "agent.task.completed.fix", "agent.task.completed:"+task.String(), &compliancev1.AgentTaskCompleted{
			TaskId: task.String(), FirmId: s.env.FirmA.String(), Agent: "fix", RunId: uuid.NewString(),
			Status: compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED, Output: out})
		if err != nil {
			t.Fatal(err)
		}
	}
	answer(task.ID, &compliancev1.FixTaskResult{Outcome: "proposed", ProposalId: proposal.String(), Changes: 1})
	deadline = time.Now().Add(10 * time.Second)
	for s.fixTask(t, inv).Status != "succeeded" {
		if time.Now().After(deadline) {
			t.Fatalf("task never completed: %+v", s.fixTask(t, inv))
		}
		time.Sleep(25 * time.Millisecond)
	}
	if got := s.fixTask(t, inv); got.Outcome != "proposed" || got.ProposalID == nil || *got.ProposalID != proposal {
		t.Fatalf("task = %+v", got)
	}

	// Now a person may ask for the LLM step: 202 with a new on-demand task and one more message.
	var started struct {
		TaskID uuid.UUID `json:"task_id"`
		Mode   string    `json:"mode"`
		Status string    `json:"status"`
	}
	s.must(t, "org-a", "alice", http.MethodPost, "/v1/invoices/"+inv.String()+"/validation/fixes", nil, http.StatusAccepted, &started)
	if started.Mode != "on_demand" || started.Status != "requested" || started.TaskID == task.ID {
		t.Fatalf("on-demand = %+v", started)
	}
	if got := s.fixTask(t, inv); got.ID != started.TaskID {
		t.Fatalf("latest task = %+v", got)
	}
	var trail struct {
		Items []struct {
			Action string `json:"action"`
		} `json:"items"`
	}
	s.must(t, "org-a", "u", http.MethodGet, "/v1/invoices/"+inv.String()+"/validation/audit", nil, http.StatusOK, &trail)
	if len(trail.Items) != 1 || trail.Items[0].Action != "invoice.fix_requested" {
		b, _ := json.Marshal(trail)
		t.Fatalf("audit = %s", b)
	}
	if n := eventstest.StreamMsgs(t, js.JS, events.AgentsStream); n != 3 { // two requests, one completion
		t.Fatalf("AGENTS holds %d messages, want 3", n)
	}
}

func TestFixAgentOffWithoutJetStream(t *testing.T) {
	s := newPropStack(t)
	inv := s.env.SeedInvoice(t, s.env.FirmA, propPayload)
	if _, err := s.svc.Run(context.Background(), s.env.FirmA, inv, validation.RunOpts{Trigger: validation.TriggerExtracted}); err != nil {
		t.Fatal(err)
	}
	code, raw := s.do(t, "org-a", "alice", http.MethodPost, "/v1/invoices/"+inv.String()+"/validation/fixes", nil)
	if code != http.StatusServiceUnavailable || !strings.Contains(string(raw), "fix_unavailable") {
		t.Fatalf("fixes without an agent = %d %s", code, raw)
	}
	if err := s.tc.Ready(context.Background()); err != nil {
		t.Fatalf("Ready without a Fix agent: %v", err)
	}
}
