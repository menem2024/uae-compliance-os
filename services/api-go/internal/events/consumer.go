package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
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

// consumerCheckInterval is how often a running consumer confirms that its
// durable still exists on the server (a backstop for a missed 409).
var consumerCheckInterval = 10 * time.Second

// RunValidationConsumer ensures the streams and the api-validation durable,
// then consumes invoice.extracted until ctx is cancelled (drains in-flight
// messages, returns nil) or consumption stops because the durable or stream
// was lost (returns an error so the caller can recreate them).
func RunValidationConsumer(ctx context.Context, js jetstream.JetStream, handle ExtractedHandler) error {
	return runValidationSession(ctx, js, handle, nil)
}

func runValidationSession(ctx context.Context, js jetstream.JetStream, handle ExtractedHandler, running *atomic.Bool) error {
	if err := EnsureStreams(ctx, js); err != nil {
		return err
	}
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
	// Any consume error (409, no responders, missed heartbeat) prompts a
	// check that the durable still exists.
	suspect := make(chan struct{}, 1)
	onErr := jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		slog.WarnContext(ctx, "validation consumer error", "durable", ValidationDurable, "err", err)
		select {
		case suspect <- struct{}{}:
		default:
		}
	})
	// Messages already being handled finish after shutdown starts.
	msgCtx := context.WithoutCancel(ctx)
	dlq := withStreamRecovery(js)
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		processMessage(msgCtx, msg, dlq, handle)
	}, onErr)
	if err != nil {
		return fmt.Errorf("consume %s: %w", ValidationDurable, err)
	}
	closed := cc.Closed()
	if running != nil {
		running.Store(true)
		defer running.Store(false)
	}
	slog.InfoContext(ctx, "validation consumer started", "durable", ValidationDurable, "subject", ExtractedSubject)

	ticker := time.NewTicker(consumerCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cc.Drain()
			select {
			case <-closed:
			case <-time.After(drainTimeout):
				cc.Stop()
				slog.Warn("validation consumer drain timed out")
			}
			return nil
		case <-closed: // nats.go stops consuming on 409 Consumer Deleted
			return fmt.Errorf("consumer %s stopped unexpectedly (durable or stream deleted?)", ValidationDurable)
		case <-suspect:
		case <-ticker.C:
		}
		if err := durableLost(ctx, cons); err != nil {
			cc.Stop()
			select {
			case <-closed:
			case <-time.After(drainTimeout):
			}
			return fmt.Errorf("consumer %s lost: %w", ValidationDurable, err)
		}
	}
}

// durableLost returns an error only when the server says the durable or its
// stream no longer exists; transient lookup failures (e.g. a reconnect) are
// not treated as loss.
func durableLost(ctx context.Context, cons jetstream.Consumer) error {
	ictx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := cons.Info(ictx)
	if errors.Is(err, jetstream.ErrConsumerNotFound) || errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}
	return nil
}

// ValidationConsumer keeps the api-validation consumer running: whenever
// consumption stops it re-ensures the streams and the durable after
// retryInterval. Ready reports its liveness for /readyz.
type ValidationConsumer struct {
	js            jetstream.JetStream
	handle        ExtractedHandler
	retryInterval time.Duration
	running       atomic.Bool
}

// NewValidationConsumer returns a supervisor for the api-validation consumer.
func NewValidationConsumer(js jetstream.JetStream, handle ExtractedHandler, retryInterval time.Duration) *ValidationConsumer {
	return &ValidationConsumer{js: js, handle: handle, retryInterval: retryInterval}
}

// Run consumes until ctx is cancelled, restarting after any failure.
func (c *ValidationConsumer) Run(ctx context.Context) {
	for {
		err := runValidationSession(ctx, c.js, c.handle, &c.running)
		if ctx.Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "validation consumer stopped, restarting", "err", err, "retry_in", c.retryInterval)
		select {
		case <-ctx.Done():
			return
		case <-time.After(c.retryInterval):
		}
	}
}

// Ready returns nil only while the consumer is consuming and its durable
// exists on the server.
func (c *ValidationConsumer) Ready(ctx context.Context) error {
	if !c.running.Load() {
		return fmt.Errorf("consumer %s not running", ValidationDurable)
	}
	if _, err := c.js.Consumer(ctx, InvoicesStream, ValidationDurable); err != nil {
		return fmt.Errorf("consumer %s: %w", ValidationDurable, err)
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
	deadLetter(ctx, msg, hdr, dlq, ev.GetInvoiceId())
}

// deadLetter publishes the raw message to the DLQ and always terminates it,
// even if the DLQ publish fails, so it is never stranded. The publish carries
// a deterministic Nats-Msg-Id so that redelivering the same invoice (e.g.
// after the durable is recreated and the retained INVOICES stream replays)
// dedupes within the DLQ stream's duplicate window instead of storing the
// same dead letter again.
func deadLetter(ctx context.Context, msg jetstream.Msg, hdr nats.Header, dlq msgPublisher, invoiceID string) {
	out := &nats.Msg{Subject: DLQExtractedSubject, Data: msg.Data(), Header: nats.Header{}}
	for k, v := range hdr {
		if strings.HasPrefix(k, "Nats-") { // server-interpreted (dedupe, expectations)
			continue
		}
		out.Header[k] = append([]string(nil), v...)
	}
	out.Header.Set(jetstream.MsgIDHeader, dlqMsgID(invoiceID, msg.Data()))
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

// dlqMsgID returns a deterministic dedup id for a dead-lettered message. It
// keys on the invoice id when known; an undecodable payload has none, so it
// falls back to a content hash (still deterministic across redelivery of the
// identical bytes).
func dlqMsgID(invoiceID string, data []byte) string {
	if invoiceID != "" {
		return DLQExtractedSubject + ":" + invoiceID
	}
	sum := sha256.Sum256(data)
	return DLQExtractedSubject + ":undecodable:" + hex.EncodeToString(sum[:])
}
