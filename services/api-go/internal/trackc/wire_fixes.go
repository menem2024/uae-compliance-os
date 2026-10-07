package trackc

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/fixtasks"
)

// fixRetryInterval is how long the api-fix-tasks durable waits before it re-creates a lost consumer.
const fixRetryInterval = 2 * time.Second

// wireFixes connects the Fix agent: runs trigger an automatic request (FIX_AGENT_AUTO), the
// POST .../fixes route requests on demand, and the api-fix-tasks consumer and the task sweeper are
// registered as the module's background work (Run). Without a JetStream handle nothing is wired:
// the route answers fix_unavailable and validation runs trigger nothing.
func wireFixes(m *Module) {
	if m.js == nil {
		return
	}
	req := &fixtasks.Requester{Pool: m.Pool, Pub: events.NewBus(m.js), Auto: m.Config.FixAgentAuto}
	m.Validation.OnRun = req.OnRun
	m.Fixes = fixAdapter{req}

	durable := events.NewDurable(m.js, fixtasks.DurableConfig(), (&fixtasks.Consumer{Pool: m.Pool}).Handle, fixRetryInterval)
	sweeper := &fixtasks.Sweeper{Pool: m.Pool, Req: req}
	m.loops = append(m.loops, durable.Run, sweeper.Run)
	m.ready = append(m.ready, durable.Ready)
}

// fixAdapter lets the route use fixtasks.Requester without fixtasks importing this package.
type fixAdapter struct{ r *fixtasks.Requester }

func (a fixAdapter) RequestOnDemand(ctx context.Context, firmID, invoiceID uuid.UUID, requestedBy string) (FixTask, error) {
	t, err := a.r.RequestOnDemand(ctx, firmID, invoiceID, requestedBy)
	switch {
	case errors.Is(err, fixtasks.ErrNoFixableIssues), errors.Is(err, fixtasks.ErrStaleRun):
		return FixTask{}, ErrNoFixableIssues
	case errors.Is(err, fixtasks.ErrFixInProgress):
		return FixTask{}, ErrFixInProgress
	case err != nil:
		return FixTask{}, err
	}
	return FixTask{ID: t.ID, Mode: t.Mode, Status: t.Status}, nil
}
