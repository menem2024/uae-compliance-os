//go:build integration

package agents_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/agents"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/dbtest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

func deliver(t *testing.T, c *agents.Consumer, subject string, m proto.Message) {
	t.Helper()
	data, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Handle(context.Background(), fakeMsg{subject: subject, data: data}); err != nil {
		t.Fatalf("%s: %v", subject, err)
	}
}

func TestConsumerIsOrderIndependentAndIdempotent(t *testing.T) {
	env := dbtest.Setup(t)
	ctx := context.Background()
	c := &agents.Consumer{Pool: env.App}
	cc := dbtest.ClientCompany(t, env, env.FirmA, "Oasis", "")
	doc := uuid.New()
	if _, err := dbtest.ExecFirm(ctx, env, env.FirmA, `INSERT INTO documents (id, firm_id, client_company_id, sha256, object_key, filename,
		content_type, size_bytes, status) VALUES ($1::uuid, $2::uuid, $3, repeat('a', 64), 'firms/' || $2::text || '/docs/' || $1::text,
		'a.pdf', 'application/pdf', 10, 'uploaded')`, doc, env.FirmA, cc); err != nil {
		t.Fatal(err)
	}
	firm := env.FirmA.String()
	old, run := uuid.NewString(), uuid.NewString()
	t0 := time.Now().Add(-time.Minute)
	started := func(id string, at time.Time) *compliancev1.AgentRunStarted {
		return &compliancev1.AgentRunStarted{RunId: id, FirmId: firm, ClientCompanyId: cc.String(), Workflow: "document_ingestion@1",
			SubjectType: "document", SubjectId: doc.String(), StartedAt: timestamppb.New(at), DeliveryAttempt: 1}
	}
	step := func(seq int64, status compliancev1.AgentStepStatus) *compliancev1.AgentStepEvent {
		return &compliancev1.AgentStepEvent{RunId: run, FirmId: firm, StepId: uuid.NewSHA1(uuid.MustParse(run), []byte("fetch#1")).String(),
			Seq: seq, NodeId: "fetch", Agent: "orchestrator", Action: "fetch", Kind: compliancev1.StepKind_STEP_KIND_DETERMINISTIC,
			Status: status, Attempt: 1, At: timestamppb.New(t0.Add(time.Duration(seq) * time.Second)),
			SubjectType: "document", SubjectId: doc.String()}
	}
	finished := &compliancev1.AgentRunFinished{RunId: run, FirmId: firm, Status: compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED,
		SubjectType: "document", SubjectId: doc.String(), Totals: &compliancev1.RunTotals{Steps: 1}, FinishedAt: timestamppb.New(t0.Add(time.Minute))}

	deliver(t, c, events.AgentRunStartedSubject, started(old, t0)) // a worker died on this one
	deliver(t, c, events.AgentRunFinishedSubject, finished)        // out of order: finished first
	deliver(t, c, events.AgentRunStepSubject, step(3, compliancev1.AgentStepStatus_AGENT_STEP_STATUS_SUCCEEDED))
	deliver(t, c, events.AgentRunStepSubject, step(2, compliancev1.AgentStepStatus_AGENT_STEP_STATUS_STARTED)) // older seq loses
	deliver(t, c, events.AgentRunStartedSubject, started(run, t0.Add(time.Second)))                            // redelivered run
	deliver(t, c, events.AgentRunStartedSubject, started(run, t0.Add(time.Second)))                            // duplicate

	var status, oldStatus, oldCode, stepStatus, docStatus string
	var seq int64
	q := func(sql string, args []any, dst ...any) {
		t.Helper()
		if err := dbtest.QueryRowFirm(ctx, env, env.FirmA, sql, args...).Scan(dst...); err != nil {
			t.Fatal(err)
		}
	}
	q(`SELECT status FROM agent_runs WHERE id = $1`, []any{run}, &status)
	q(`SELECT status, error_code FROM agent_runs WHERE id = $1`, []any{old}, &oldStatus, &oldCode)
	q(`SELECT status, seq FROM agent_steps WHERE run_id = $1`, []any{run}, &stepStatus, &seq)
	q(`SELECT status FROM documents WHERE id = $1`, []any{doc}, &docStatus)
	if status != "succeeded" || oldStatus != "abandoned" || oldCode != "superseded" || stepStatus != "succeeded" || seq != 3 || docStatus != "processing" {
		t.Fatalf("run=%s old=%s/%s step=%s/%d doc=%s", status, oldStatus, oldCode, stepStatus, seq, docStatus)
	}
	// A Firm-B event can never land in Firm A's rows: RLS WITH CHECK applies to the message's firm.
	deliver(t, c, events.AgentRunStartedSubject, &compliancev1.AgentRunStarted{RunId: uuid.NewString(), FirmId: env.FirmB.String(),
		Workflow: "w@1", SubjectType: "document", SubjectId: doc.String(), StartedAt: timestamppb.Now()})
	q(`SELECT status FROM documents WHERE id = $1`, []any{doc}, &docStatus)
	if docStatus != "processing" {
		t.Fatal("a Firm B event changed a Firm A document")
	}
	// Proposals: insert once, redelivery is a no-op.
	pc := &compliancev1.ProposalCreated{Proposal: &compliancev1.Proposal{ProposalId: uuid.NewString(), FirmId: firm, RunId: run,
		Agent: "intake", Kind: "document.attribution", TargetType: "document", TargetId: doc.String(), SummaryKey: "k", Confidence: 0.8}}
	deliver(t, c, events.AgentProposalCreatedSubject, pc)
	deliver(t, c, events.AgentProposalCreatedSubject, pc)
	var n int
	q(`SELECT count(*) FROM proposals WHERE run_id = $1`, []any{run}, &n)
	if n != 1 {
		t.Fatalf("proposals %d", n)
	}
}
