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
)

// ProtoPublisher publishes one protobuf message with a Nats-Msg-Id and waits
// for the JetStream ack. *Bus implements it; tests use a fake.
type ProtoPublisher interface {
	Publish(ctx context.Context, subject, msgID string, m proto.Message) error
}

// Bus is the Track B publisher: every message carries a Nats-Msg-Id (dedupe
// inside the stream's duplicate window) and the W3C trace context.
type Bus struct{ js msgPublisher }

// NewBus wraps a JetStream handle; a publish that finds its stream missing
// re-creates the streams and retries once.
func NewBus(js msgPublisher) *Bus { return &Bus{js: withStreamRecovery(js)} }

// Publish marshals m and publishes it under a PRODUCER span, waiting up to 5 s for the ack.
func (b *Bus) Publish(ctx context.Context, subject, msgID string, m proto.Message) error {
	data, err := proto.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", subject, err)
	}
	ctx, span := otel.Tracer(tracerName).Start(ctx, subject+" publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", subject),
			attribute.String("messaging.message.id", msgID),
		))
	defer span.End()
	msg := &nats.Msg{Subject: subject, Data: data, Header: nats.Header{}}
	otel.GetTextMapPropagator().Inject(ctx, HeaderCarrier(msg.Header))
	pubCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := b.js.PublishMsg(pubCtx, msg, jetstream.WithMsgID(msgID)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("publish %s: %w", subject, err)
	}
	return nil
}

// Tap holds a core (non-durable, at-most-once) subscription, e.g. on agent.>
// for the live SSE feed (contract section 4). The caller unsubscribes.
func Tap(nc *nats.Conn, subject string, h func(subject string, data []byte)) (*nats.Subscription, error) {
	sub, err := nc.Subscribe(subject, func(m *nats.Msg) { h(m.Subject, m.Data) })
	if err != nil {
		return nil, fmt.Errorf("tap %s: %w", subject, err)
	}
	if err := sub.SetPendingLimits(8192, 64<<20); err != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("tap %s limits: %w", subject, err)
	}
	return sub, nil
}
