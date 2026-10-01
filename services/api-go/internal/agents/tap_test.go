package agents_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/agents"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
)

func startTap(t *testing.T) (*agents.Hub, *eventstest.JetStream) {
	t.Helper()
	js := eventstest.StartJetStream(t)
	hub := agents.NewHub(20, 64)
	tap := &agents.LiveTap{Hub: hub}
	stop, err := tap.Start(js.Conn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(stop)
	return hub, &js
}

func TestLiveTapPublishesStepEvent(t *testing.T) {
	hub, js := startTap(t)
	firm := uuid.New()
	sub, err := hub.Subscribe(firm)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	m := &compliancev1.AgentStepEvent{
		RunId: uuid.NewString(), FirmId: firm.String(), StepId: uuid.NewString(),
		Seq: 1, NodeId: "fetch", Agent: "orchestrator", Action: "fetch",
		Kind: compliancev1.StepKind_STEP_KIND_DETERMINISTIC, Status: compliancev1.AgentStepStatus_AGENT_STEP_STATUS_SUCCEEDED,
		Attempt: 1, At: timestamppb.Now(), SubjectType: "document", SubjectId: uuid.NewString(),
	}
	data, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := js.Conn.Publish(events.AgentRunStepSubject, data); err != nil {
		t.Fatal(err)
	}
	if err := js.Conn.Flush(); err != nil {
		t.Fatal(err)
	}

	select {
	case ev := <-sub.Events():
		if ev.Type != "step" || ev.Firm != firm {
			t.Fatalf("got %+v, want type=step firm=%s", ev, firm)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the tap to publish")
	}
}

func TestLiveTapIgnoresUndecodablePayload(t *testing.T) {
	hub, js := startTap(t)
	firm := uuid.New()
	sub, err := hub.Subscribe(firm)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	// An over-long varint: proto.Unmarshal must return an error, not a zero-value message, exercising
	// the "undecodable payload is logged and never calls Publish" path.
	garbage := []byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}
	if err := js.Conn.Publish(events.AgentRunStepSubject, garbage); err != nil {
		t.Fatal(err)
	}
	if err := js.Conn.Flush(); err != nil {
		t.Fatal(err)
	}

	select {
	case ev, ok := <-sub.Events():
		t.Fatalf("undecodable payload must not reach Publish: %+v ok=%v", ev, ok)
	case <-time.After(200 * time.Millisecond):
		// expected: nothing published
	}
}
