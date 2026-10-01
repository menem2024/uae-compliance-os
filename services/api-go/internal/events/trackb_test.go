package events_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
	"github.com/menem2024/uae-platform/services/api-go/internal/events"
	"github.com/menem2024/uae-platform/services/api-go/internal/events/eventstest"
)

func ctx5(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestEnsureStreamsCreatesTrackBStreams(t *testing.T) {
	js := eventstest.StartJetStream(t).JS
	for name, age := range map[string]time.Duration{events.DocumentsStream: 30 * 24 * time.Hour, events.AgentsStream: 7 * 24 * time.Hour} {
		s, err := js.Stream(ctx5(t), name)
		if err != nil {
			t.Fatal(err)
		}
		cfg := s.CachedInfo().Config
		if cfg.MaxAge != age || cfg.Duplicates != 2*time.Minute || cfg.Storage != jetstream.FileStorage {
			t.Errorf("%s: max_age=%v duplicates=%v storage=%v", name, cfg.MaxAge, cfg.Duplicates, cfg.Storage)
		}
	}
}

func TestBusPublishDedupesByMsgID(t *testing.T) {
	js := eventstest.StartJetStream(t).JS
	bus := events.NewBus(js)
	ev := &compliancev1.DocumentFailed{DocumentId: "d1", ReasonCode: "internal"}
	for range 2 {
		if err := bus.Publish(ctx5(t), events.DocumentFailedSubject, "document.failed:d1:r1", ev); err != nil {
			t.Fatal(err)
		}
	}
	if n := eventstest.StreamMsgs(t, js, events.DocumentsStream); n != 1 {
		t.Fatalf("stream holds %d messages, want 1", n)
	}
}

type recorder struct {
	mu   sync.Mutex
	seen map[string]int
}

func (r *recorder) add(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.seen == nil {
		r.seen = map[string]int{}
	}
	r.seen[id]++
	return r.seen[id]
}

func (r *recorder) count(id string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen[id]
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func startDurable(t *testing.T, js jetstream.JetStream, workers int, h events.MsgHandler) *events.Durable {
	t.Helper()
	d := events.NewDurable(js, events.DurableConfig{
		Stream: events.DocumentsStream, Name: "test-docs",
		FilterSubjects: []string{events.DocumentExtractedSubject, events.DocumentFailedSubject},
		DLQSubject:     "dlq.document.results", MaxDeliver: 2, AckWait: time.Second, NakDelay: 10 * time.Millisecond,
		HandleTimeout: 500 * time.Millisecond, Workers: workers,
	}, h, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return d
}

func decodeID(msg jetstream.Msg) string {
	var ev compliancev1.DocumentFailed
	_ = proto.Unmarshal(msg.Data(), &ev)
	return ev.GetDocumentId()
}

func TestDurableAcksNaksAndDeadLetters(t *testing.T) {
	js := eventstest.StartJetStream(t).JS
	var rec recorder
	startDurable(t, js, 2, func(_ context.Context, msg jetstream.Msg) error {
		id := decodeID(msg)
		n := rec.add(id)
		switch id {
		case "perm":
			return fmt.Errorf("%w: bad payload", events.ErrPermanent)
		case "flaky":
			return errors.New("still down")
		case "panics":
			if n == 1 {
				panic("boom")
			}
		}
		return nil
	})
	bus := events.NewBus(js)
	for _, id := range []string{"ok", "perm", "flaky", "panics"} {
		if err := bus.Publish(ctx5(t), events.DocumentFailedSubject, "document.failed:"+id+":r", &compliancev1.DocumentFailed{DocumentId: id}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "dead letters", func() bool { return eventstest.StreamMsgs(t, js, events.DLQStream) == 2 })
	if rec.count("ok") != 1 || rec.count("perm") != 1 || rec.count("flaky") != 2 || rec.count("panics") != 2 {
		t.Fatalf("deliveries: %v", rec.seen)
	}
	s, err := js.Stream(ctx5(t), events.DLQStream)
	if err != nil {
		t.Fatal(err)
	}
	m, err := s.GetLastMsgForSubject(ctx5(t), "dlq.document.results")
	if err != nil {
		t.Fatal(err)
	}
	if id := m.Header.Get(jetstream.MsgIDHeader); id != "dlq.document.results:document.failed:flaky:r" && id != "dlq.document.results:document.failed:perm:r" {
		t.Fatalf("dlq msg id %q", id)
	}
}

func TestDurableWorkerPoolIsBounded(t *testing.T) {
	js := eventstest.StartJetStream(t).JS
	var active, peak, done atomic.Int32
	startDurable(t, js, 2, func(context.Context, jetstream.Msg) error {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		active.Add(-1)
		done.Add(1)
		return nil
	})
	bus := events.NewBus(js)
	for i := range 8 {
		id := fmt.Sprintf("d%d", i)
		if err := bus.Publish(ctx5(t), events.DocumentExtractedSubject, "document.extracted:"+id+":r", &compliancev1.DocumentFailed{DocumentId: id}); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "all handled", func() bool { return done.Load() == 8 })
	if peak.Load() > 2 {
		t.Fatalf("peak concurrency %d > 2 workers", peak.Load())
	}
}

func TestDurableRecoversAfterDurableDeleted(t *testing.T) {
	js := eventstest.StartJetStream(t).JS
	var rec recorder
	d := startDurable(t, js, 1, func(_ context.Context, msg jetstream.Msg) error { rec.add(decodeID(msg)); return nil })
	waitFor(t, "ready", func() bool { return d.Ready(ctx5(t)) == nil })
	if err := js.DeleteConsumer(ctx5(t), events.DocumentsStream, "test-docs"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "ready again", func() bool { return d.Ready(ctx5(t)) == nil })
	if err := events.NewBus(js).Publish(ctx5(t), events.DocumentFailedSubject, "document.failed:after:r", &compliancev1.DocumentFailed{DocumentId: "after"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "message after recovery", func() bool { return rec.count("after") == 1 })
}

func TestTapSeesJetStreamPublishes(t *testing.T) {
	st := eventstest.StartJetStream(t)
	got := make(chan string, 1)
	sub, err := events.Tap(st.Conn, "agent.>", func(subject string, _ []byte) { got <- subject })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	if err := st.Conn.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := events.NewBus(st.JS).Publish(ctx5(t), events.AgentRunStepSubject, "agent.run.step:r1:1", &compliancev1.AgentStepEvent{RunId: "r1", Seq: 1}); err != nil {
		t.Fatal(err)
	}
	select {
	case s := <-got:
		if s != events.AgentRunStepSubject {
			t.Fatalf("subject %s", s)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tap saw nothing")
	}
}

func TestDLQMsgID(t *testing.T) {
	if got := events.DLQMsgID("dlq.x", "orig", nil); got != "dlq.x:orig" {
		t.Fatal(got)
	}
	if a, b := events.DLQMsgID("dlq.x", "", []byte("a")), events.DLQMsgID("dlq.x", "", []byte("a")); a != b || len(a) != len("dlq.x:sha256:")+64 {
		t.Fatal(a, b)
	}
}
