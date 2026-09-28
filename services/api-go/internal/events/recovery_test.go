package events

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
)

// startJetStream runs an in-process JetStream server and returns a
// connected handle with the streams ensured.
func startJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()
	srv, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	t.Cleanup(func() { srv.Shutdown(); srv.WaitForShutdown() })
	nc, js, err := Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := EnsureStreams(ctx, js); err != nil {
		t.Fatal(err)
	}
	return js
}

func publishExtracted(t *testing.T, js jetstream.JetStream, invoiceID string) {
	t.Helper()
	b, err := proto.Marshal(&compliancev1.InvoiceExtracted{InvoiceId: invoiceID, FirmId: "f1", Invoice: &compliancev1.Invoice{}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := js.Publish(ctx, ExtractedSubject, b); err != nil {
		t.Fatal(err)
	}
}

func recordingHandler() (ExtractedHandler, <-chan string) {
	got := make(chan string, 16)
	return func(_ context.Context, ev *compliancev1.InvoiceExtracted) error {
		got <- ev.GetInvoiceId()
		return nil
	}, got
}

// waitProcessed waits until want is handled. Other ids are tolerated: a
// recreated durable replays the retained stream (at-least-once delivery).
func waitProcessed(t *testing.T, got <-chan string, want string) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case id := <-got:
			if id == want {
				return
			}
		case <-timeout:
			t.Fatalf("invoice %q not processed within 10s", want)
		}
	}
}

func TestRunValidationConsumerReturnsWhenDurableDeleted(t *testing.T) {
	js := startJetStream(t)
	handle, got := recordingHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunValidationConsumer(ctx, js, handle) }()

	publishExtracted(t, js, "inv-1")
	waitProcessed(t, got, "inv-1")

	if err := js.DeleteConsumer(context.Background(), InvoicesStream, ValidationDurable); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("consumer stopped without an error after its durable was deleted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunValidationConsumer kept blocking after its durable was deleted")
	}
}

func TestPublishRecreatesLostStream(t *testing.T) {
	js := startJetStream(t)
	ctx := context.Background()
	if err := js.DeleteStream(ctx, InvoicesStream); err != nil {
		t.Fatal(err)
	}
	ev := &compliancev1.InvoiceSubmitted{InvoiceId: "inv-1", FirmId: "f1", Invoice: &compliancev1.Invoice{}}
	if err := NewPublisher(js).PublishSubmitted(ctx, ev); err != nil {
		t.Fatalf("publish after INVOICES was lost: %v", err)
	}
	s, err := js.Stream(ctx, InvoicesStream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != 1 {
		t.Fatalf("INVOICES has %d messages, want 1", info.State.Msgs)
	}
}

func waitReady(t *testing.T, c *ValidationConsumer, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		err := c.Ready(context.Background())
		if (err == nil) == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Ready() = %v, want ready=%v", err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestValidationConsumerRecoversAndReportsReadiness(t *testing.T) {
	for name, lose := range map[string]func(jetstream.JetStream) error{
		"durable deleted": func(js jetstream.JetStream) error {
			return js.DeleteConsumer(context.Background(), InvoicesStream, ValidationDurable)
		},
		"INVOICES stream lost": func(js jetstream.JetStream) error {
			return js.DeleteStream(context.Background(), InvoicesStream)
		},
	} {
		t.Run(name, func(t *testing.T) {
			js := startJetStream(t)
			handle, got := recordingHandler()
			c := NewValidationConsumer(js, handle, 500*time.Millisecond)
			if err := c.Ready(context.Background()); err == nil {
				t.Fatal("ready before the consumer started")
			}
			ctx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			go func() { c.Run(ctx); close(stopped) }()
			t.Cleanup(func() { cancel(); <-stopped })

			waitReady(t, c, true)
			publishExtracted(t, js, "inv-1")
			waitProcessed(t, got, "inv-1")

			if err := lose(js); err != nil {
				t.Fatal(err)
			}
			// The durable is gone right now, before the supervisor's retry.
			if err := c.Ready(context.Background()); err == nil {
				t.Fatal("ready although the durable is gone")
			}

			// The supervisor re-ensures the stream and durable; processing resumes.
			waitReady(t, c, true)
			publishExtracted(t, js, "inv-2")
			waitProcessed(t, got, "inv-2")

			cancel()
			select {
			case <-stopped:
			case <-time.After(5 * time.Second):
				t.Fatal("Run did not return after cancel")
			}
			if err := c.Ready(context.Background()); err == nil {
				t.Fatal("ready after the consumer stopped")
			}
		})
	}
}

// A server restart that loses all JetStream state never sends 409 Consumer
// Deleted; the consumer must notice via its error handler / liveness check.
func TestValidationConsumerRecoversAfterServerLosesState(t *testing.T) {
	opts := &server.Options{Host: "127.0.0.1", Port: -1, JetStream: true, StoreDir: t.TempDir(), NoLog: true, NoSigs: true}
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Start()
	if !srv.ReadyForConnections(10 * time.Second) {
		t.Fatal("nats server not ready")
	}
	nc, js, err := Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)

	handle, got := recordingHandler()
	c := NewValidationConsumer(js, handle, 200*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { c.Run(ctx); close(stopped) }()
	t.Cleanup(func() { cancel(); <-stopped })
	waitReady(t, c, true)
	publishExtracted(t, js, "inv-1")
	waitProcessed(t, got, "inv-1")

	// Restart on the same port with an empty store: streams and durable are gone.
	port := srv.Addr().(*net.TCPAddr).Port
	srv.Shutdown()
	srv.WaitForShutdown()
	opts2 := *opts
	opts2.Port, opts2.StoreDir = port, t.TempDir()
	srv2, err := server.NewServer(&opts2)
	if err != nil {
		t.Fatal(err)
	}
	go srv2.Start()
	if !srv2.ReadyForConnections(10 * time.Second) {
		t.Fatal("restarted nats server not ready")
	}
	t.Cleanup(func() { srv2.Shutdown(); srv2.WaitForShutdown() })

	waitReady(t, c, true) // stream and durable recreated
	publishExtracted(t, js, "inv-2")
	waitProcessed(t, got, "inv-2")
	cancel() // stop before the server so the drain does not wait on a dead server
	<-stopped
}
