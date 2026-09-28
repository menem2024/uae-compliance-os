package events

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
)

// TestEnsureStreamsBoundsInvoicesRetention verifies INVOICES gets a bounded
// MaxAge, so recreating a lost durable (which redelivers everything JetStream
// still retains, DeliverAllPolicy) cannot replay further back than that.
func TestEnsureStreamsBoundsInvoicesRetention(t *testing.T) {
	js := startJetStream(t) // already calls EnsureStreams once
	s, err := js.Stream(context.Background(), InvoicesStream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.MaxAge != invoicesMaxAge {
		t.Fatalf("INVOICES MaxAge = %s, want %s", info.Config.MaxAge, invoicesMaxAge)
	}
	if invoicesMaxAge <= 0 || invoicesMaxAge > 30*24*time.Hour {
		t.Fatalf("invoicesMaxAge = %s is not a sane bound (~7 days)", invoicesMaxAge)
	}
}

// TestEnsureStreamsIsCompatibleWithAnExistingUnboundedStream verifies
// EnsureStreams can update-or-create: applying it to a stream that was
// created (or last updated) without a MaxAge must not error, and must add
// the bound going forward.
func TestEnsureStreamsIsCompatibleWithAnExistingUnboundedStream(t *testing.T) {
	js := startJetStream(t)
	ctx := context.Background()
	// Simulate a pre-existing INVOICES stream with unbounded retention, as an
	// older api-go version would have created it.
	unbounded := jetstream.StreamConfig{Name: InvoicesStream, Subjects: []string{"invoice.>"}, Storage: jetstream.FileStorage}
	if _, err := js.CreateOrUpdateStream(ctx, unbounded); err != nil {
		t.Fatal(err)
	}

	if err := EnsureStreams(ctx, js); err != nil {
		t.Fatalf("EnsureStreams on a pre-existing unbounded stream: %v", err)
	}
	s, err := js.Stream(ctx, InvoicesStream)
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.MaxAge != invoicesMaxAge {
		t.Fatalf("MaxAge after update-or-create = %s, want %s", info.Config.MaxAge, invoicesMaxAge)
	}
}

func TestPublishSubmittedInjectsProducerSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

	ctx, parent := tp.Tracer("test").Start(context.Background(), "http")
	js := &fakeDLQ{}
	ev := &compliancev1.InvoiceSubmitted{InvoiceId: "i1", FirmId: "f1", Invoice: &compliancev1.Invoice{TotalAmount: "1050.00"}}
	if err := NewPublisher(js).PublishSubmitted(ctx, ev); err != nil {
		t.Fatal(err)
	}
	parent.End()

	if len(js.got) != 1 || js.got[0].Subject != SubmittedSubject {
		t.Fatalf("published %+v", js.got)
	}
	var got compliancev1.InvoiceSubmitted
	if err := proto.Unmarshal(js.got[0].Data, &got); err != nil || got.InvoiceId != "i1" || got.Invoice.GetTotalAmount() != "1050.00" {
		t.Fatalf("payload %+v %v", &got, err)
	}
	sc := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(), HeaderCarrier(js.got[0].Header)))
	if sc.TraceID() != parent.SpanContext().TraceID() {
		t.Fatalf("traceparent %q not in parent trace", js.got[0].Header.Get("traceparent"))
	}
	var producer sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "invoice.submitted publish" {
			producer = s
		}
	}
	if producer == nil || producer.SpanKind() != trace.SpanKindProducer || producer.SpanContext().SpanID() != sc.SpanID() {
		t.Fatalf("producer span missing or not the injected span: %+v", producer)
	}
}
