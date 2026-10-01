package agents_test

import (
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/menem2024/uae-platform/services/api-go/internal/agents"
)

func TestHubTooManyStreams(t *testing.T) {
	h := agents.NewHub(20, 64)
	firm := uuid.New()
	for i := 0; i < 20; i++ {
		if _, err := h.Subscribe(firm); err != nil {
			t.Fatalf("subscribe %d: %v", i, err)
		}
	}
	if _, err := h.Subscribe(firm); err != agents.ErrTooManyStreams {
		t.Fatalf("21st subscribe: got %v, want ErrTooManyStreams", err)
	}
}

func TestHubFirmIsolation(t *testing.T) {
	h := agents.NewHub(20, 64)
	firmA, firmB := uuid.New(), uuid.New()
	subA, err := h.Subscribe(firmA)
	if err != nil {
		t.Fatal(err)
	}
	defer subA.Close()
	subB, err := h.Subscribe(firmB)
	if err != nil {
		t.Fatal(err)
	}
	defer subB.Close()

	h.Publish(firmA, agents.Event{Type: "step", ID: "1-x"})

	select {
	case ev := <-subA.Events():
		if ev.ID != "1-x" {
			t.Fatalf("firm A got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("firm A subscriber got nothing")
	}
	select {
	case ev, ok := <-subB.Events():
		t.Fatalf("firm B subscriber must never see firm A's event: %+v ok=%v", ev, ok)
	case <-time.After(50 * time.Millisecond):
		// expected: nothing arrives for firm B
	}
}

func TestHubFullBufferDropsThenResyncs(t *testing.T) {
	const buf = 4
	h := agents.NewHub(20, buf)
	firm := uuid.New()
	sub, err := h.Subscribe(firm)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()

	// Fill the buffer exactly, then overflow it once.
	for i := 0; i < buf; i++ {
		h.Publish(firm, agents.Event{Type: "step", ID: "fill"})
	}
	h.Publish(firm, agents.Event{Type: "step", ID: "dropped-1"}) // buffer full: dropped, dropped flag set
	h.Publish(firm, agents.Event{Type: "step", ID: "dropped-2"}) // still full: this one never reaches the chan either

	// Drain the buffer; the stale deliveries that made it in are the "fill" ones.
	for i := 0; i < buf; i++ {
		select {
		case ev := <-sub.Events():
			if ev.ID != "fill" {
				t.Fatalf("expected a fill event, got %+v", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out draining buffer")
		}
	}
	// Now the channel has room: the next Publish after a drop must be a resync, not the real event.
	h.Publish(firm, agents.Event{Type: "step", ID: "real"})
	select {
	case ev := <-sub.Events():
		if ev.Type != "resync" {
			t.Fatalf("expected resync after a drop, got %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for resync")
	}
}

func TestHubCloseDuringConcurrentPublish(t *testing.T) {
	h := agents.NewHub(20, 64)
	firm := uuid.New()
	sub, err := h.Subscribe(firm)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				h.Publish(firm, agents.Event{Type: "step", ID: "x"})
			}
		}
	}()
	// Drain concurrently so sends don't just pile into the buffer before Close.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range sub.Events() {
		}
	}()

	time.Sleep(10 * time.Millisecond)
	sub.Close()
	sub.Close() // Close is idempotent
	close(stop)
	wg.Wait()
}
