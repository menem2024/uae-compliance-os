package fixtasks

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/db"
	"github.com/menem2024/uae-platform/services/api-go/internal/db/sqlc"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/proposals"
)

// Consumer records the Fix agent's completions on fix_tasks. It is the handler of the api-fix-tasks
// durable (events.NewDurable): nil acks, an error wrapping events.ErrPermanent dead-letters to
// DLQSubject at once, any other error is retried until MaxDeliver.
type Consumer struct {
	Pool *pgxpool.Pool
}

func permanent(format string, args ...any) error {
	return fmt.Errorf("%w: %s", events.ErrPermanent, fmt.Sprintf(format, args...))
}

// completion is a decoded AgentTaskCompleted: the Firm to write under and the terminal update.
type completion struct {
	firm   uuid.UUID
	params sqlc.TrackCCompleteFixTaskParams
}

var outcomes = map[string]bool{"proposed": true, "no_fix": true, "not_improving": true}

var statuses = map[compliancev1.AgentRunStatus]string{
	compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED:       "succeeded",
	compliancev1.AgentRunStatus_AGENT_RUN_STATUS_FAILED:          "failed",
	compliancev1.AgentRunStatus_AGENT_RUN_STATUS_BUDGET_EXCEEDED: "budget_exceeded",
	compliancev1.AgentRunStatus_AGENT_RUN_STATUS_CANCELLED:       "cancelled",
}

// completionOf maps an AgentTaskCompleted to the fix_tasks update. Every defect that no redelivery can
// repair (undecodable ids, an unknown status or outcome, a missing or foreign output) is permanent.
func completionOf(m *compliancev1.AgentTaskCompleted) (completion, error) {
	task, err := uuid.Parse(m.GetTaskId())
	if err != nil {
		return completion{}, permanent("task_id %q", m.GetTaskId())
	}
	firm, err := uuid.Parse(m.GetFirmId())
	if err != nil {
		return completion{}, permanent("firm_id %q", m.GetFirmId())
	}
	if m.GetAgent() != Agent {
		return completion{}, permanent("agent %q on the fix subject", m.GetAgent())
	}
	status, ok := statuses[m.GetStatus()]
	if !ok {
		return completion{}, permanent("status %v is not terminal", m.GetStatus())
	}
	c := completion{firm: firm, params: sqlc.TrackCCompleteFixTaskParams{ID: task, Status: status, ErrorCode: m.GetErrorCode()}}
	if run, err := uuid.Parse(m.GetRunId()); err == nil {
		c.params.AgentRunID = run
	}
	if m.GetStatus() != compliancev1.AgentRunStatus_AGENT_RUN_STATUS_SUCCEEDED {
		return c, nil
	}
	if m.GetOutput() == nil {
		return completion{}, permanent("task %s succeeded without an output", task)
	}
	var res compliancev1.FixTaskResult
	if err := m.GetOutput().UnmarshalTo(&res); err != nil {
		return completion{}, permanent("task %s output: %v", task, err)
	}
	if !outcomes[res.GetOutcome()] {
		return completion{}, permanent("task %s outcome %q", task, res.GetOutcome())
	}
	c.params.Outcome = res.GetOutcome()
	if res.GetProposalId() != "" {
		p, err := uuid.Parse(res.GetProposalId())
		if err != nil {
			return completion{}, permanent("task %s proposal_id %q", task, res.GetProposalId())
		}
		c.params.ProposalID = p
	}
	return c, nil
}

// Handle is the events.MsgHandler of api-fix-tasks.
func (c *Consumer) Handle(ctx context.Context, msg jetstream.Msg) error {
	if msg.Subject() != SubjectCompleted {
		return permanent("unexpected subject %s", msg.Subject())
	}
	var m compliancev1.AgentTaskCompleted
	if err := proto.Unmarshal(msg.Data(), &m); err != nil {
		return permanent("decode AgentTaskCompleted: %v", err)
	}
	comp, err := completionOf(&m)
	if err != nil {
		return err
	}
	var n int64
	err = db.WithFirm(ctx, c.Pool, comp.firm, func(q *sqlc.Queries) error {
		var qerr error
		n, qerr = q.TrackCCompleteFixTask(ctx, comp.params)
		return qerr
	})
	switch {
	case err != nil && proposals.IsPermanentPG(err):
		return fmt.Errorf("%w: %w", events.ErrPermanent, err)
	case err != nil:
		return fmt.Errorf("complete fix task %s: %w", comp.params.ID, err)
	case n == 0:
		// A duplicate, a task another Firm owns, or one the sweeper already closed: nothing to do.
		slog.InfoContext(ctx, "fixtasks: completion changed no row, acknowledging", "task_id", comp.params.ID, "status", comp.params.Status)
	}
	return nil
}
