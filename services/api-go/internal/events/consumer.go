package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// Consumer settings for invoice.extracted.
const (
	ValidationDurable = "api-validation"
	MaxDeliver        = 5
	ackWait           = 30 * time.Second
	nakDelay          = 2 * time.Second
	handleTimeout     = 25 * time.Second // below ackWait
	drainTimeout      = 3 * time.Second
)

// ErrPermanent marks a handler failure that redelivery cannot fix; the
// message is dead-lettered immediately.
var ErrPermanent = errors.New("permanent failure")

// ExtractedHandler processes one invoice.extracted event.
type ExtractedHandler func(ctx context.Context, ev *compliancev1.InvoiceExtracted) error

// RunValidationConsumer consumes invoice.extracted with durable
// api-validation until ctx is cancelled, then drains in-flight messages.
func RunValidationConsumer(ctx context.Context, js jetstream.JetStream, handle ExtractedHandler) error {
	cons, err := js.CreateOrUpdateConsumer(ctx, InvoicesStream, jetstream.ConsumerConfig{
		Durable:       ValidationDurable,
		FilterSubject: ExtractedSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    MaxDeliver,
		AckWait:       ackWait,
	})
	if err != nil {
		return fmt.Errorf("ensure consumer %s: %w", ValidationDurable, err)
	}
	// Messages already being handled finish after shutdown starts.
	msgCtx := context.WithoutCancel(ctx)
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		processMessage(msgCtx, msg, js, handle)
	})
	if err != nil {
		return fmt.Errorf("consume %s: %w", ValidationDurable, err)
	}
	slog.InfoContext(ctx, "validation consumer started", "durable", ValidationDurable, "subject", ExtractedSubject)
	<-ctx.Done()
	cc.Drain()
	select {
	case <-cc.Closed():
	case <-time.After(drainTimeout):
		cc.Stop()
		slog.Warn("validation consumer drain timed out")
	}
	return nil
}

func processMessage(ctx context.Context, msg jetstream.Msg, dlq msgPublisher, handle ExtractedHandler) {
	hdr := msg.Headers()
	if hdr == nil {
		hdr = nats.Header{}
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, HeaderCarrier(hdr))
	ctx, span := otel.Tracer(tracerName).Start(ctx, ExtractedSubject+" process",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", msg.Subject()),
			attribute.String("messaging.consumer.group.name", ValidationDurable),
		))
	defer span.End()
	ctx, cancel := context.WithTimeout(ctx, handleTimeout)
	defer cancel()

	var ev compliancev1.InvoiceExtracted
	err := proto.Unmarshal(msg.Data(), &ev)
	if err != nil {
		err = fmt.Errorf("%w: decode InvoiceExtracted: %w", ErrPermanent, err)
	} else {
		span.SetAttributes(attribute.String("invoice.id", ev.GetInvoiceId()), attribute.String("firm.id", ev.GetFirmId()))
		err = handle(ctx, &ev)
	}
	if err == nil {
		if ackErr := msg.Ack(); ackErr != nil {
			slog.ErrorContext(ctx, "ack failed", "invoice_id", ev.GetInvoiceId(), "err", ackErr)
		}
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	var delivered uint64
	if meta, mErr := msg.Metadata(); mErr == nil {
		delivered = meta.NumDelivered
	} else {
		slog.WarnContext(ctx, "message metadata unavailable", "err", mErr)
	}
	if !errors.Is(err, ErrPermanent) && delivered < MaxDeliver {
		slog.WarnContext(ctx, "handling invoice.extracted failed, will retry",
			"invoice_id", ev.GetInvoiceId(), "delivered", delivered, "err", err)
		if nakErr := msg.NakWithDelay(nakDelay); nakErr != nil {
			slog.ErrorContext(ctx, "nak failed", "invoice_id", ev.GetInvoiceId(), "err", nakErr)
		}
		return
	}
	slog.ErrorContext(ctx, "dead-lettering invoice.extracted",
		"invoice_id", ev.GetInvoiceId(), "firm_id", ev.GetFirmId(), "delivered", delivered,
		"permanent", errors.Is(err, ErrPermanent), "err", err)
	deadLetter(ctx, msg, hdr, dlq)
}

// deadLetter publishes the raw message to the DLQ and always terminates it,
// even if the DLQ publish fails, so it is never stranded.
func deadLetter(ctx context.Context, msg jetstream.Msg, hdr nats.Header, dlq msgPublisher) {
	out := &nats.Msg{Subject: DLQExtractedSubject, Data: msg.Data(), Header: nats.Header{}}
	for k, v := range hdr {
		if strings.HasPrefix(k, "Nats-") { // server-interpreted (dedupe, expectations)
			continue
		}
		out.Header[k] = append([]string(nil), v...)
	}
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := dlq.PublishMsg(pubCtx, out); err != nil {
		slog.ErrorContext(ctx, "dead-letter publish failed; terminating anyway",
			"subject", DLQExtractedSubject, "err", err)
	}
	if err := msg.Term(); err != nil {
		slog.ErrorContext(ctx, "term failed", "err", err)
	}
}
