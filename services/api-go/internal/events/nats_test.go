package events

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
)

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
