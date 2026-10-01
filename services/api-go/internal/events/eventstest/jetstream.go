// Package eventstest runs an in-process JetStream server for tests of any
// package (the Phase 0 helper in events is package-private).
package eventstest

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/menem2024/uae-platform/services/api-go/internal/events"
)

// JetStream is a running embedded server with a connected client.
type JetStream struct {
	Server *server.Server
	Conn   *nats.Conn
	JS     jetstream.JetStream
	URL    string
}

// StartJetStream starts a JetStream server on a random loopback port with its
// store in t.TempDir(), ensures every stream, and shuts it down at cleanup.
func StartJetStream(t testing.TB) JetStream {
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
	nc, js, err := events.Connect(srv.ClientURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := events.EnsureStreams(ctx, js); err != nil {
		t.Fatal(err)
	}
	return JetStream{Server: srv, Conn: nc, JS: js, URL: srv.ClientURL()}
}

// StreamMsgs returns how many messages stream holds (DLQ assertions).
func StreamMsgs(t testing.TB, js jetstream.JetStream, stream string) uint64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := js.Stream(ctx, stream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return info.State.Msgs
}
