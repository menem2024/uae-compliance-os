package agents

import (
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// LiveTap subscribes once per process to agent.> (core, at-most-once: Task 9's events.Tap) and projects
// each message into Hub.Publish. It never touches Postgres; Task 16's consumer stays the source of
// truth and this feed is best-effort. A message that cannot be decoded is logged and dropped.
type LiveTap struct {
	Hub *Hub
}

// Start subscribes nc to agent.> and returns an unsubscribe func.
func (t *LiveTap) Start(nc *nats.Conn) (func(), error) {
	sub, err := events.Tap(nc, "agent.>", t.handle)
	if err != nil {
		return nil, err
	}
	return func() { _ = sub.Unsubscribe() }, nil
}

func (t *LiveTap) handle(subject string, data []byte) {
	switch subject {
	case events.AgentRunStartedSubject:
		m := &compliancev1.AgentRunStarted{}
		if !t.unmarshal(subject, data, m) {
			return
		}
		t.publishRun(m.GetFirmId(), m.GetRunId(), runViewFromStarted(m), m.GetStartedAt().AsTime())
	case events.AgentRunStepSubject:
		m := &compliancev1.AgentStepEvent{}
		if !t.unmarshal(subject, data, m) {
			return
		}
		if _, err := uuid.Parse(m.GetRunId()); err != nil {
			slog.Error("agents tap: bad run_id on step", "subject", subject)
			return
		}
		t.publishStep(m.GetFirmId(), m.GetStepId(), stepViewFromProto(m), m.GetAt().AsTime())
	case events.AgentRunFinishedSubject:
		m := &compliancev1.AgentRunFinished{}
		if !t.unmarshal(subject, data, m) {
			return
		}
		t.publishRun(m.GetFirmId(), m.GetRunId(), runViewFromFinished(m), m.GetFinishedAt().AsTime())
	case events.AgentProposalCreatedSubject:
		m := &compliancev1.ProposalCreated{}
		if !t.unmarshal(subject, data, m) || m.GetProposal() == nil {
			return
		}
		p := m.GetProposal()
		t.publishProposal(p.GetFirmId(), p.GetProposalId(), proposalViewFromProto(p), p.GetCreatedAt().AsTime())
	default:
		// agent.task.* and anything new: not projected to the feed.
	}
}

func (t *LiveTap) unmarshal(subject string, data []byte, m proto.Message) bool {
	if err := proto.Unmarshal(data, m); err != nil {
		slog.Error("agents tap: decode", "subject", subject, "err", err)
		return false
	}
	return true
}

// sseEventID builds the SSE `id:` from an id taken raw from NATS. NATS carries no auth, so an id with a
// CR/LF would let a publisher inject frames into every subscribed Firm's stream: only a UUID (re-rendered
// canonically) is accepted, anything else drops the event.
func sseEventID(what, raw string, at time.Time) (string, bool) {
	id, err := uuid.Parse(raw)
	if err != nil {
		slog.Error("agents tap: dropping event, id is not a uuid", "kind", what, "len", len(raw))
		return "", false
	}
	return EventID(id.String(), at), true
}

func (t *LiveTap) publishRun(firm, id string, v RunView, at time.Time) {
	firmID, err := uuid.Parse(firm)
	if err != nil {
		slog.Error("agents tap: bad firm_id", "firm_id", firm)
		return
	}
	eventID, ok := sseEventID("run", id, at)
	if !ok {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("agents tap: encode run view", "err", err)
		return
	}
	t.Hub.Publish(firmID, Event{Type: "run", ID: eventID, Firm: firmID, Data: data})
}

func (t *LiveTap) publishStep(firm, id string, v StepView, at time.Time) {
	firmID, err := uuid.Parse(firm)
	if err != nil {
		slog.Error("agents tap: bad firm_id", "firm_id", firm)
		return
	}
	eventID, ok := sseEventID("step", id, at)
	if !ok {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("agents tap: encode step view", "err", err)
		return
	}
	t.Hub.Publish(firmID, Event{Type: "step", ID: eventID, Firm: firmID, Data: data})
}

func (t *LiveTap) publishProposal(firm, id string, v proposals.Proposal, at time.Time) {
	firmID, err := uuid.Parse(firm)
	if err != nil {
		slog.Error("agents tap: bad firm_id", "firm_id", firm)
		return
	}
	eventID, ok := sseEventID("proposal", id, at)
	if !ok {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		slog.Error("agents tap: encode proposal view", "err", err)
		return
	}
	t.Hub.Publish(firmID, Event{Type: "proposal", ID: eventID, Firm: firmID, Data: data})
}

// ------------------------------------------------------------------ proto -> partial view (pure)

func runViewFromStarted(m *compliancev1.AgentRunStarted) RunView {
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
	return RunView{
		ID: m.GetRunId(), Workflow: m.GetWorkflow(), SubjectType: m.GetSubjectType(), SubjectID: m.GetSubjectId(),
		ClientCompanyID: m.GetClientCompanyId(), Status: "running", Plan: plan,
		Budget: RunBudget{MaxSteps: b.GetMaxSteps(), MaxLLMCalls: b.GetMaxLlmCalls(),
			MaxInputTokens: b.GetMaxInputTokens(), MaxOutputTokens: b.GetMaxOutputTokens(),
			MaxCostMicroUSD: b.GetMaxCostMicroUsd(), DeadlineSeconds: b.GetDeadlineSeconds()},
		StartedAt: m.GetStartedAt().AsTime(),
	}
}

func runViewFromFinished(m *compliancev1.AgentRunFinished) RunView {
	t := m.GetTotals()
	finished := m.GetFinishedAt().AsTime()
	return RunView{
		ID: m.GetRunId(), SubjectType: m.GetSubjectType(), SubjectID: m.GetSubjectId(),
		Status: RunStatus(m.GetStatus()), ErrorCode: m.GetErrorCode(), Plan: []PlanNode{},
		Totals: TotalsView{Steps: t.GetSteps(), LlmCalls: t.GetLlmCalls(),
			ResponseCacheHits: t.GetResponseCacheHits(), InputTokens: t.GetInputTokens(),
			OutputTokens: t.GetOutputTokens(), CostMicroUsd: t.GetCostMicroUsd()},
		FinishedAt: &finished,
	}
}

func stepViewFromProto(m *compliancev1.AgentStepEvent) StepView {
	args := m.GetMessageArgs()
	if args == nil {
		args = map[string]string{}
	}
	deps := m.GetDependsOn()
	if deps == nil {
		deps = []string{}
	}
	v := StepView{
		ID: m.GetStepId(), RunID: m.GetRunId(), NodeID: m.GetNodeId(), Agent: m.GetAgent(), Action: m.GetAction(),
		Kind: Kind(m.GetKind()), Status: StepStatus(m.GetStatus()), MessageKey: m.GetMessageKey(),
		ErrorCode: m.GetErrorCode(), SubjectType: m.GetSubjectType(), SubjectID: m.GetSubjectId(),
		ClientCompanyID: m.GetClientCompanyId(), DependsOn: deps, Seq: m.GetSeq(), Attempt: m.GetAttempt(),
		At: m.GetAt().AsTime(), DurationMs: m.GetDurationMs(), MessageArgs: args,
	}
	if u := m.GetUsage(); u != nil && (u.GetModel() != "" || u.GetLlmCalls() > 0) {
		v.Usage = &UsageView{Model: u.GetModel(), PromptID: u.GetPromptId(), PromptVersion: u.GetPromptVersion(),
			InputTokens: u.GetInputTokens(), OutputTokens: u.GetOutputTokens(),
			CacheReadInputTokens: u.GetCacheReadInputTokens(), CacheCreationInputTokens: u.GetCacheCreationInputTokens(),
			CostMicroUsd: u.GetCostMicroUsd(), ResponseCacheHit: u.GetResponseCacheHit(), LlmCalls: u.GetLlmCalls()}
	}
	return v
}

func proposalViewFromProto(p *compliancev1.Proposal) proposals.Proposal {
	out := proposals.Proposal{Agent: p.GetAgent(), Kind: p.GetKind(), TargetType: p.GetTargetType(),
		SummaryKey: p.GetSummaryKey(), Rationale: p.GetRationale(), Confidence: p.GetConfidence(),
		State: proposals.StateProposed, SummaryArgs: map[string]string{}, Changes: []proposals.Change{},
		Evidence: []proposals.Evidence{}}
	if id, err := uuid.Parse(p.GetProposalId()); err == nil {
		out.ID = id
	}
	if id, err := uuid.Parse(p.GetFirmId()); err == nil {
		out.FirmID = id
	}
	if id, err := uuid.Parse(p.GetTargetId()); err == nil {
		out.TargetID = id
	}
	if cc, err := uuid.Parse(p.GetClientCompanyId()); err == nil {
		out.ClientCompanyID = &cc
	}
	if run, err := uuid.Parse(p.GetRunId()); err == nil {
		out.RunID = &run
	}
	if args := p.GetSummaryArgs(); args != nil {
		out.SummaryArgs = args
	}
	for _, c := range p.GetChanges() {
		out.Changes = append(out.Changes, proposals.Change{Path: c.GetPath(), OldValue: c.GetOldValue(),
			NewValue: c.GetNewValue()})
	}
	for _, e := range p.GetEvidence() {
		out.Evidence = append(out.Evidence, proposals.Evidence{Kind: e.GetKind(), Ref: e.GetRef(),
			Excerpt: e.GetExcerpt()})
	}
	if p.GetCreatedAt().IsValid() {
		created := p.GetCreatedAt().AsTime()
		out.CreatedAt = created
	}
	if p.GetExpiresAt().IsValid() {
		exp := p.GetExpiresAt().AsTime()
		out.ExpiresAt = &exp
	}
	return out
}
