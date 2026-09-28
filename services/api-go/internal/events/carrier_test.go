package events

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestHeaderCarrierRoundTrip(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	sid, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	h := nats.Header{}
	prop := propagation.TraceContext{}
	prop.Inject(ctx, HeaderCarrier(h))
	if h.Get("traceparent") == "" {
		t.Fatal("traceparent not injected")
	}
	got := trace.SpanContextFromContext(prop.Extract(context.Background(), HeaderCarrier(h)))
	if got.TraceID() != tid {
		t.Fatalf("trace id = %s, want %s", got.TraceID(), tid)
	}
}
