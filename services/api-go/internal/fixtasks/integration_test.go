//go:build integration

// Fix task requests over a real Postgres 17 (forced RLS) and an embedded JetStream server: one row and
// one message per (run, mode), idempotent publishes, the api-fix-tasks consumer with its dead letter,
// and the sweeper. The agent side is a stand-in: the tests publish AgentTaskCompleted themselves.
package fixtasks_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/audit"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/trackctest"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
	"github.com/menem2024/uae-platform/services/api-go/internal/fieldpath"
	"github.com/menem2024/uae-platform/services/api-go/internal/fixtasks"
	"github.com/menem2024/uae-platform/services/api-go/internal/invoicefix"
	"github.com/menem2024/uae-platform/services/api-go/internal/validation"
)

const payload = `{"invoice_number":"INV-1","issue_date":"2026-09-27","seller_trn":"100000000000003","currency":"aed","total_amount":"1050.00"}`

// scripted answers Validate with a fixed set of issues.
type scripted struct {
	issues []*compliancev1.ValidationIssue
}

func (s scripted) Validate(context.Context, *connect.Request[compliancev1.ValidateRequest]) (*connect.Response[compliancev1.ValidateResponse], error) {
	return connect.NewResponse(&compliancev1.ValidateResponse{Run: &compliancev1.ValidationRun{
		RulesetVersion: "pint-ae@1.0.4+r1", RulesEvaluated: 300, Issues: s.issues}}), nil
}

func iss(rule string, sev compliancev1.Severity, path string, fixable bool) *compliancev1.ValidationIssue {
	return &compliancev1.ValidationIssue{RuleId: rule, Severity: sev, Path: path, BusinessTerm: "BT-1", Message: "m " + rule,
		MessageAr: "ر", Fixable: fixable, SuggestedValue: "S", MessageArgs: map[string]string{"expected": "AED"}}
}

var mixedIssues = []*compliancev1.ValidationIssue{
	iss("ibr-cl-04", compliancev1.Severity_SEVERITY_ERROR, "currency", true),      // sent
	iss("ibr-001", compliancev1.Severity_SEVERITY_ERROR, "invoice_number", false), // not fixable
	iss("ibr-w-01", compliancev1.Severity_SEVERITY_WARNING, "buyer.name", true),   // not an error
}

type fixture struct {
	env     trackctest.Env
	js      eventstest.JetStream
	bus     *events.Bus
	svc     *validation.Service
	req     *fixtasks.Requester
	invoice uuid.UUID
	run     validation.RunResult
}

func setup(t *testing.T, issues []*compliancev1.ValidationIssue) *fixture {
	t.Helper()
	env := trackctest.Setup(t)
	js := eventstest.StartJetStream(t)
	f := &fixture{env: env, js: js, bus: events.NewBus(js.JS)}
	f.svc = &validation.Service{Pool: env.App, Validator: scripted{issues}}
	f.req = &fixtasks.Requester{Pool: env.App, Pub: f.bus, Auto: true}
	f.invoice = env.SeedInvoice(t, env.FirmA, payload)
	res, err := f.svc.Run(context.Background(), env.FirmA, f.invoice, validation.RunOpts{Trigger: validation.TriggerExtracted})
	if err != nil {
		t.Fatal(err)
	}
	f.run = res
	return f
}

// published returns every agent.task.requested message in the AGENTS stream, oldest first.
func (f *fixture) published(t *testing.T) []*jetstream.RawStreamMsg {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st, err := f.js.JS.Stream(ctx, events.AgentsStream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := st.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var out []*jetstream.RawStreamMsg
	if info.State.Msgs == 0 {
		return out
	}
	for seq := info.State.FirstSeq; seq <= info.State.LastSeq; seq++ {
		m, err := st.GetMsg(ctx, seq)
		if err != nil {
			continue // a gap
		}
		if m.Subject == events.AgentTaskRequestedSubject {
			out = append(out, m)
		}
	}
	return out
}

type taskRow struct {
	ID                                 uuid.UUID
	Mode, Status, Outcome, ErrCode, By string
	ProposalID, AgentRunID             *uuid.UUID
	Published, Completed               bool
}

func (f *fixture) tasks(t *testing.T, firm uuid.UUID) []taskRow {
	t.Helper()
	var out []taskRow
	err := f.env.AppTx(context.Background(), firm, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT id, mode, status, outcome, error_code, requested_by, proposal_id, agent_run_id,
			published_at IS NOT NULL, completed_at IS NOT NULL FROM fix_tasks ORDER BY requested_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r taskRow
			if err := rows.Scan(&r.ID, &r.Mode, &r.Status, &r.Outcome, &r.ErrCode, &r.By, &r.ProposalID, &r.AgentRunID, &r.Published, &r.Completed); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *fixture) only(t *testing.T) taskRow {
	t.Helper()
	rows := f.tasks(t, f.env.FirmA)
	if len(rows) != 1 {
		t.Fatalf("fix tasks = %+v", rows)
	}
	return rows[0]
}

// failingPub fails its first n publishes, then delegates.
type failingPub struct {
	mu    sync.Mutex
	fails int
	next  events.ProtoPublisher
}

func (p *failingPub) Publish(ctx context.Context, subject, id string, m proto.Message) error {
	p.mu.Lock()
	if p.fails > 0 {
		p.fails--
		p.mu.Unlock()
		return errors.New("nats down")
	}
	p.mu.Unlock()
	return p.next.Publish(ctx, subject, id, m)
}

func TestRequestOnDemandPublishesTheExactTask(t *testing.T) {
	f := setup(t, mixedIssues)
	task, err := f.req.RequestOnDemand(context.Background(), f.env.FirmA, f.invoice, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if task.Mode != "on_demand" || task.Status != "requested" || task.RunID != f.run.RunID || task.PublishedAt == nil {
		t.Fatalf("task = %+v", task)
	}
	row := f.only(t)
	if row.ID != task.ID || row.Mode != "on_demand" || row.By != "alice" || !row.Published || row.Completed {
		t.Fatalf("row = %+v", row)
	}
	msgs := f.published(t)
	if len(msgs) != 1 {
		t.Fatalf("published %d messages", len(msgs))
	}
	if got := msgs[0].Header.Get(nats.MsgIdHdr); got != "agent.task.requested:"+task.ID.String() {
		t.Errorf("Nats-Msg-Id = %q", got)
	}
	var m compliancev1.AgentTaskRequested
	if err := proto.Unmarshal(msgs[0].Data, &m); err != nil {
		t.Fatal(err)
	}
	if m.GetTaskId() != task.ID.String() || m.GetFirmId() != f.env.FirmA.String() || m.GetAgent() != "fix" || m.GetSubjectType() != "invoice" ||
		m.GetSubjectId() != f.invoice.String() || m.GetRequestedBy() != "alice" || m.GetClientCompanyId() != "" {
		t.Errorf("message = %v", &m)
	}
	want := &compliancev1.RunBudget{MaxLlmCalls: 2, MaxCostMicroUsd: 50000, MaxSteps: 16, MaxInputTokens: 40000, MaxOutputTokens: 4000, DeadlineSeconds: 120}
	if !proto.Equal(m.GetBudget(), want) {
		t.Errorf("budget = %v", m.GetBudget())
	}
	var in compliancev1.FixTaskInput
	if err := anypb.UnmarshalTo(m.GetInput(), &in, proto.UnmarshalOptions{}); err != nil {
		t.Fatal(err)
	}
	if in.GetInvoiceId() != f.invoice.String() || in.GetPayloadVersion() != 1 || in.GetValidationRunId() != f.run.RunID.String() ||
		in.GetRulesetVersion() != "pint-ae@1.0.4+r1" || in.GetInvoice().GetCurrency() != "aed" || in.GetInvoice().GetInvoiceNumber() != "INV-1" {
		t.Errorf("input = %v", &in)
	}
	if len(in.GetIssues()) != 1 || in.GetIssues()[0].GetRuleId() != "ibr-cl-04" || !in.GetIssues()[0].GetFixable() ||
		in.GetIssues()[0].GetMessageArgs()["expected"] != "AED" {
		t.Errorf("input issues = %v (only fixable error issues go to the agent)", in.GetIssues())
	}
}

func TestRequestsAreIdempotentPerRunAndMode(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	f.req.OnRun(ctx, f.env.FirmA, f.invoice, f.run)
	f.req.OnRun(ctx, f.env.FirmA, f.invoice, f.run) // the same run again: same row, same message
	row := f.only(t)
	if row.Mode != "auto" || row.By != "system" || !row.Published {
		t.Fatalf("row = %+v", row)
	}
	if n := len(f.published(t)); n != 1 {
		t.Fatalf("published %d messages for one (run, mode)", n)
	}
	// The auto task still runs: an on-demand request for the same run waits.
	if _, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice"); !errors.Is(err, fixtasks.ErrFixInProgress) {
		t.Fatalf("on-demand while auto runs: %v", err)
	}
	// Once the auto task ended, a person may ask again; one more task, one more message.
	complete(t, f, row.ID, compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED, "no_fix", "")
	task, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice")
	if err != nil || task.Mode != "on_demand" || task.ID == row.ID {
		t.Fatalf("on-demand after auto: %+v, %v", task, err)
	}
	if _, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice"); !errors.Is(err, fixtasks.ErrFixInProgress) {
		t.Fatalf("second on-demand while the first runs: %v", err)
	}
	if n := len(f.published(t)); n != 2 {
		t.Fatalf("published %d messages", n)
	}
	if n := len(f.tasks(t, f.env.FirmA)); n != 2 {
		t.Fatalf("tasks = %d", n)
	}
}

// complete records a completion straight through the consumer's handler.
func complete(t *testing.T, f *fixture, task uuid.UUID, status compliancev1.AgentRunStatus, outcome, proposal string) {
	t.Helper()
	m := &compliancev1.AgentTaskCompleted{TaskId: task.String(), FirmId: f.env.FirmA.String(), Agent: "fix", RunId: uuid.NewString(), Status: status}
	if status == compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED {
		out, err := anypb.New(&compliancev1.FixTaskResult{Outcome: outcome, ProposalId: proposal})
		if err != nil {
			t.Fatal(err)
		}
		m.Output = out
	}
	// Apply it the way the durable would, synchronously.
	data, _ := proto.Marshal(m)
	c := &fixtasks.Consumer{Pool: f.env.App}
	if err := c.Handle(context.Background(), msgOf{subject: fixtasks.SubjectCompleted, data: data}); err != nil {
		t.Fatal(err)
	}
}

type msgOf struct {
	subject string
	data    []byte
	jetstream.Msg
}

func (m msgOf) Subject() string { return m.subject }
func (m msgOf) Data() []byte    { return m.data }

func TestNatsMsgIdDeduplicatesAPublishRetry(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	task, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice")
	if err != nil {
		t.Fatal(err)
	}
	// The same message again (a retry after a lost ack): the stream keeps one copy.
	if err := f.bus.Publish(ctx, events.AgentTaskRequestedSubject, fixtasks.MsgID(task.ID), &compliancev1.AgentTaskRequested{TaskId: task.ID.String()}); err != nil {
		t.Fatal(err)
	}
	if n := len(f.published(t)); n != 1 {
		t.Fatalf("stream holds %d copies", n)
	}
	if n := len(f.tasks(t, f.env.FirmA)); n != 1 {
		t.Fatalf("fix tasks = %d", n)
	}
}

func TestOnRunHonoursTheSwitchAndTheIssues(t *testing.T) {
	ctx := context.Background()
	// FIX_AGENT_AUTO=false: no row, no message.
	f := setup(t, mixedIssues)
	off := &fixtasks.Requester{Pool: f.env.App, Pub: f.bus, Auto: false}
	off.OnRun(ctx, f.env.FirmA, f.invoice, f.run)
	if n := len(f.tasks(t, f.env.FirmA)); n != 0 || len(f.published(t)) != 0 {
		t.Fatalf("Auto=false requested a fix (%d tasks)", n)
	}
	// Auto on, but the run has only an unfixable error and a fixable warning: nothing to ask.
	g := setup(t, mixedIssues[1:])
	g.req.OnRun(ctx, g.env.FirmA, g.invoice, g.run)
	if n := len(g.tasks(t, g.env.FirmA)); n != 0 {
		t.Fatalf("a run without fixable errors requested a fix (%d tasks)", n)
	}
	if _, err := g.req.RequestOnDemand(ctx, g.env.FirmA, g.invoice, "alice"); !errors.Is(err, fixtasks.ErrNoFixableIssues) {
		t.Fatalf("on-demand without fixable errors: %v", err)
	}
}

func TestRequestForAStaleRunIsRefused(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	// A person edits the invoice after the run: its issues describe an older payload.
	if err := db.WithFirm(ctx, f.env.App, f.env.FirmA, func(q *sqlc.Queries) error {
		_, err := invoicefix.Apply(ctx, q, f.env.FirmA, f.invoice, 0, []fieldpath.FieldChange{{Path: "total_amount", OldValue: "1050.00", NewValue: "1050.50"}},
			audit.Actor{Type: "user", ID: "u"}, false, "invoice.fields_changed", "typo")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice"); !errors.Is(err, fixtasks.ErrStaleRun) {
		t.Fatalf("err = %v", err)
	}
	if n := len(f.tasks(t, f.env.FirmA)); n != 0 {
		t.Fatalf("tasks = %d", n)
	}
}

func TestAnotherFirmCannotRequestForOurInvoice(t *testing.T) {
	f := setup(t, mixedIssues)
	if _, err := f.req.RequestOnDemand(context.Background(), f.env.FirmB, f.invoice, "mallory"); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if n := len(f.tasks(t, f.env.FirmA)) + len(f.tasks(t, f.env.FirmB)); n != 0 {
		t.Fatalf("tasks = %d", n)
	}
}

func TestSweeperRepublishesAnUnpublishedTask(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	flaky := &fixtasks.Requester{Pool: f.env.App, Pub: &failingPub{fails: 1, next: f.bus}, Auto: true}
	task, err := flaky.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice")
	if err != nil {
		t.Fatalf("a failed publish must not fail the request: %v", err)
	}
	if task.PublishedAt != nil || f.only(t).Published || len(f.published(t)) != 0 {
		t.Fatal("task looks published")
	}
	sw := &fixtasks.Sweeper{Pool: f.env.App, Req: flaky, PublishAfter: time.Millisecond, TimeoutAfter: time.Hour}
	time.Sleep(20 * time.Millisecond)
	republished, closed, err := sw.Sweep(ctx)
	if err != nil || republished < 1 {
		t.Fatalf("sweep = %d, %d, %v", republished, closed, err)
	}
	if !f.only(t).Published || len(f.published(t)) != 1 {
		t.Fatalf("after sweep: %+v, %d messages", f.only(t), len(f.published(t)))
	}
	if _, _, err := sw.Sweep(ctx); err != nil || len(f.published(t)) != 1 {
		t.Fatalf("a second sweep published again: %v, %d messages", err, len(f.published(t)))
	}
}

func TestSweeperClosesAnUnpublishedTaskWhoseInvoiceMovedOn(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	flaky := &fixtasks.Requester{Pool: f.env.App, Pub: &failingPub{fails: 1, next: f.bus}, Auto: true}
	if _, err := flaky.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := db.WithFirm(ctx, f.env.App, f.env.FirmA, func(q *sqlc.Queries) error {
		_, err := invoicefix.Apply(ctx, q, f.env.FirmA, f.invoice, 0, []fieldpath.FieldChange{{Path: "total_amount", OldValue: "1050.00", NewValue: "1050.50"}},
			audit.Actor{Type: "user", ID: "u"}, false, "invoice.fields_changed", "typo")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sw := &fixtasks.Sweeper{Pool: f.env.App, Req: flaky, PublishAfter: time.Millisecond, TimeoutAfter: time.Hour}
	time.Sleep(20 * time.Millisecond)
	if _, _, err := sw.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	row := f.only(t)
	if row.Status != "failed" || row.ErrCode != "stale_run" || len(f.published(t)) != 0 {
		t.Fatalf("row = %+v, %d messages", row, len(f.published(t)))
	}
}

func TestSweeperTimesOutATaskTheAgentNeverAnswered(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	task, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice")
	if err != nil {
		t.Fatal(err)
	}
	sw := &fixtasks.Sweeper{Pool: f.env.App, Req: f.req, PublishAfter: time.Hour, TimeoutAfter: time.Millisecond}
	time.Sleep(20 * time.Millisecond)
	republished, closed, err := sw.Sweep(ctx)
	if err != nil || closed < 1 {
		t.Fatalf("sweep = %d, %d, %v", republished, closed, err)
	}
	row := f.only(t)
	if row.ID != task.ID || row.Status != "failed" || row.ErrCode != "timeout" || !row.Completed {
		t.Fatalf("row = %+v", row)
	}
	// A late answer for the closed task changes nothing and is acknowledged.
	complete(t, f, task.ID, compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED, "proposed", uuid.NewString())
	if got := f.only(t); got.Status != "failed" || got.Outcome != "" || got.ProposalID != nil {
		t.Fatalf("a late completion changed the closed task: %+v", got)
	}
	// ... and a person can ask again once a new run exists; for the same run the closed task is returned.
	again, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice")
	if err != nil || again.ID != task.ID || again.Status != "failed" {
		t.Fatalf("again = %+v, %v", again, err)
	}
}

// runDurable starts api-fix-tasks on the embedded server.
func runDurable(t *testing.T, f *fixture) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	d := events.NewDurable(f.js.JS, fixtasks.DurableConfig(), (&fixtasks.Consumer{Pool: f.env.App}).Handle, 100*time.Millisecond)
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(10 * time.Second)
	for d.Ready(ctx) != nil {
		if time.Now().After(deadline) {
			t.Fatal("api-fix-tasks never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func publishCompleted(t *testing.T, f *fixture, firm, task uuid.UUID, id string, status compliancev1.AgentRunStatus, res *compliancev1.FixTaskResult, code string) {
	t.Helper()
	m := &compliancev1.AgentTaskCompleted{TaskId: task.String(), FirmId: firm.String(), Agent: "fix", RunId: uuid.NewString(), Status: status, ErrorCode: code}
	if res != nil {
		a, err := anypb.New(res)
		if err != nil {
			t.Fatal(err)
		}
		m.Output = a
	}
	if err := f.bus.Publish(context.Background(), fixtasks.SubjectCompleted, id, m); err != nil {
		t.Fatal(err)
	}
}

func TestDurableRecordsCompletionsAndDeadLettersTheUnreadable(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	runDurable(t, f)
	task, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice")
	if err != nil {
		t.Fatal(err)
	}

	// Garbage on the subject goes to the dead letter queue and does not block the next message.
	if _, err := f.js.JS.Publish(ctx, fixtasks.SubjectCompleted, []byte{0xff, 0xfe, 0x01}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the dead letter", func() bool { return eventstest.StreamMsgs(t, f.js.JS, events.DLQStream) == 1 })
	dlq, err := f.js.JS.Stream(ctx, events.DLQStream)
	if err != nil {
		t.Fatal(err)
	}
	last, err := dlq.GetLastMsgForSubject(ctx, fixtasks.DLQSubject)
	if err != nil || len(last.Data) != 3 {
		t.Fatalf("dlq message = %+v, %v", last, err)
	}

	// A completion from another Firm cannot close our task; it is acknowledged, not retried.
	publishCompleted(t, f, f.env.FirmB, task.ID, "agent.task.completed:foreign", compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED,
		&compliancev1.FixTaskResult{Outcome: "no_fix"}, "")

	proposal := uuid.New()
	publishCompleted(t, f, f.env.FirmA, task.ID, "agent.task.completed:"+task.ID.String(), compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED,
		&compliancev1.FixTaskResult{Outcome: "proposed", ProposalId: proposal.String(), Changes: 1}, "")
	waitFor(t, "the task to complete", func() bool { return f.only(t).Status == "succeeded" })
	row := f.only(t)
	if row.Outcome != "proposed" || row.ProposalID == nil || *row.ProposalID != proposal || row.AgentRunID == nil || !row.Completed {
		t.Fatalf("row = %+v", row)
	}

	// A redelivery of a different message for the finished task is a duplicate: acknowledged, no change.
	publishCompleted(t, f, f.env.FirmA, task.ID, "agent.task.completed:dup", compliancev1.AgentRunStatus_AGENT_RUN_STATUS_FAILED, nil, "boom")
	// A message that can never apply (unknown outcome) is dead-lettered too.
	publishCompleted(t, f, f.env.FirmA, task.ID, "agent.task.completed:bad", compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED,
		&compliancev1.FixTaskResult{Outcome: "fixed_everything"}, "")
	waitFor(t, "the second dead letter", func() bool { return eventstest.StreamMsgs(t, f.js.JS, events.DLQStream) == 2 })
	if got := f.only(t); got.Status != "succeeded" || got.Outcome != "proposed" || got.ErrCode != "" {
		t.Fatalf("a duplicate changed the finished task: %+v", got)
	}
	if n := len(f.tasks(t, f.env.FirmB)); n != 0 {
		t.Fatalf("firm B tasks = %d", n)
	}
}

func TestDurableRecordsFailureAndBudgetStatuses(t *testing.T) {
	f := setup(t, mixedIssues)
	ctx := context.Background()
	runDurable(t, f)
	task, err := f.req.RequestOnDemand(ctx, f.env.FirmA, f.invoice, "alice")
	if err != nil {
		t.Fatal(err)
	}
	publishCompleted(t, f, f.env.FirmA, task.ID, "agent.task.completed:"+task.ID.String(), compliancev1.AgentRunStatus_AGENT_RUN_STATUS_BUDGET_EXCEEDED, nil, "budget_exceeded")
	waitFor(t, "the task to close", func() bool { return f.only(t).Status != "requested" })
	if row := f.only(t); row.Status != "budget_exceeded" || row.Outcome != "" || row.ErrCode != "budget_exceeded" {
		t.Fatalf("row = %+v", row)
	}
}
