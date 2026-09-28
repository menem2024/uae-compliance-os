// Package events connects to NATS JetStream, publishes domain events and
// runs the api-validation consumer, propagating W3C trace context in headers.
package events

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
)

// NATS names shared with ai-py (see plan Global Constraints).
const (
	InvoicesStream      = "INVOICES"
	DLQStream           = "DLQ"
	SubmittedSubject    = "invoice.submitted"
	ExtractedSubject    = "invoice.extracted"
	DLQExtractedSubject = "dlq.invoice.extracted"
)

const tracerName = "github.com/menem2024/uae-platform/services/api-go/internal/events"

// Connect dials NATS and returns a JetStream handle. It fails fast if the
// server is unreachable; callers retry.
func Connect(url string) (*nats.Conn, jetstream.JetStream, error) {
	nc, err := nats.Connect(url,
		nats.Name("api-go"),
		nats.Timeout(3*time.Second),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(time.Second),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("nats connect %s: %w", url, err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("jetstream: %w", err)
	}
	return nc, js, nil
}

// EnsureStreams idempotently creates or updates the INVOICES and DLQ streams.
func EnsureStreams(ctx context.Context, js jetstream.JetStream) error {
	for _, cfg := range []jetstream.StreamConfig{
		{Name: InvoicesStream, Subjects: []string{"invoice.>"}, Storage: jetstream.FileStorage},
		{Name: DLQStream, Subjects: []string{"dlq.>"}, Storage: jetstream.FileStorage},
	} {
		if _, err := js.CreateOrUpdateStream(ctx, cfg); err != nil {
			return fmt.Errorf("ensure stream %s: %w", cfg.Name, err)
		}
	}
	return nil
}

// msgPublisher is the subset of jetstream.JetStream used for publishing.
type msgPublisher interface {
	PublishMsg(ctx context.Context, msg *nats.Msg, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Publisher publishes invoice events to JetStream.
type Publisher struct{ js msgPublisher }

// NewPublisher wraps a JetStream handle (or any PublishMsg implementation).
func NewPublisher(js msgPublisher) *Publisher { return &Publisher{js: js} }

// PublishSubmitted publishes invoice.submitted under a PRODUCER span and
// injects its trace context into the NATS headers.
func (p *Publisher) PublishSubmitted(ctx context.Context, ev *compliancev1.InvoiceSubmitted) error {
	data, err := proto.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal InvoiceSubmitted: %w", err)
	}
	ctx, span := otel.Tracer(tracerName).Start(ctx, SubmittedSubject+" publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", SubmittedSubject),
			attribute.String("invoice.id", ev.GetInvoiceId()),
		))
	defer span.End()

	msg := &nats.Msg{Subject: SubmittedSubject, Data: data, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(ctx, HeaderCarrier(msg.Header))
	pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := p.js.PublishMsg(pubCtx, msg, jetstream.WithMsgID(SubmittedSubject+":"+ev.GetInvoiceId())); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("publish %s: %w", SubmittedSubject, err)
	}
	return nil
}
