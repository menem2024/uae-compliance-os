package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// MsgHandler handles one JetStream message. nil acks it. An error wrapping
// ErrPermanent dead-letters it at once; any other error naks it with
// DurableConfig.NakDelay until MaxDeliver deliveries, then dead-letters it.
type MsgHandler func(ctx context.Context, msg jetstream.Msg) error

// DurableConfig describes one durable pull consumer.
type DurableConfig struct {
	Stream         string
	Name           string
	FilterSubjects []string
	DLQSubject     string
	MaxDeliver     int           // default 5
	AckWait        time.Duration // default 30 s
	NakDelay       time.Duration // default 2 s
	HandleTimeout  time.Duration // default AckWait - 5 s
	Workers        int           // concurrent handlers, default 4
}

func (c DurableConfig) withDefaults() DurableConfig {
	if c.MaxDeliver == 0 {
		c.MaxDeliver = MaxDeliver
	}
	if c.AckWait == 0 {
		c.AckWait = ackWait
	}
	if c.NakDelay == 0 {
		c.NakDelay = nakDelay
	}
	if c.HandleTimeout == 0 {
		c.HandleTimeout = c.AckWait - 5*time.Second
	}
	if c.Workers == 0 {
		c.Workers = 4
	}
	return c
}

// Durable keeps one durable consumer running with a bounded worker pool, the
// generic form of ValidationConsumer: whenever consumption stops (durable or
// stream lost) it re-ensures the streams and the durable after retryInterval.
type Durable struct {
	js            jetstream.JetStream
	cfg           DurableConfig
	handle        MsgHandler
	retryInterval time.Duration
	running       atomic.Bool
}

// NewDurable returns a supervisor for cfg; call Run in its own goroutine.
func NewDurable(js jetstream.JetStream, cfg DurableConfig, handle MsgHandler, retryInterval time.Duration) *Durable {
	return &Durable{js: js, cfg: cfg.withDefaults(), handle: handle, retryInterval: retryInterval}
}

// Name is the durable name.
func (d *Durable) Name() string { return d.cfg.Name }

// Run consumes until ctx is cancelled, restarting after any failure.
func (d *Durable) Run(ctx context.Context) {
	for {
		err := d.session(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "durable consumer stopped, restarting", "durable", d.cfg.Name, "err", err,
			"retry_in", d.retryInterval)
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.retryInterval):
		}
	}
}

// Ready returns nil only while the consumer is consuming and its durable
// exists on the server.
func (d *Durable) Ready(ctx context.Context) error {
	if !d.running.Load() {
		return fmt.Errorf("consumer %s not running", d.cfg.Name)
	}
	if _, err := d.js.Consumer(ctx, d.cfg.Stream, d.cfg.Name); err != nil {
		return fmt.Errorf("consumer %s: %w", d.cfg.Name, err)
	}
	return nil
}

func (d *Durable) session(ctx context.Context) error {
	if err := EnsureStreams(ctx, d.js); err != nil {
		return err
	}
	cons, err := d.js.CreateOrUpdateConsumer(ctx, d.cfg.Stream, jetstream.ConsumerConfig{
		Durable:        d.cfg.Name,
		FilterSubjects: d.cfg.FilterSubjects,
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxDeliver:     d.cfg.MaxDeliver,
		AckWait:        d.cfg.AckWait,
	})
	if err != nil {
		return fmt.Errorf("ensure consumer %s: %w", d.cfg.Name, err)
	}
	suspect := make(chan struct{}, 1)
	onErr := jetstream.ConsumeErrHandler(func(_ jetstream.ConsumeContext, err error) {
		slog.WarnContext(ctx, "consumer error", "durable", d.cfg.Name, "err", err)
		select {
		case suspect <- struct{}{}:
		default:
		}
	})
	msgCtx := context.WithoutCancel(ctx) // in-flight handlers finish after shutdown starts
	dlq := withStreamRecovery(d.js)
	sem := make(chan struct{}, d.cfg.Workers)
	var inflight sync.WaitGroup
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		sem <- struct{}{} // backpressure: at most Workers handlers at a time
		inflight.Add(1)
		go func() {
			defer func() { <-sem; inflight.Done() }()
			d.process(msgCtx, msg, dlq)
		}()
	}, onErr, jetstream.PullMaxMessages(d.cfg.Workers))
	if err != nil {
		return fmt.Errorf("consume %s: %w", d.cfg.Name, err)
	}
	closed := cc.Closed()
	d.running.Store(true)
	defer d.running.Store(false)
	slog.InfoContext(ctx, "durable consumer started", "durable", d.cfg.Name, "subjects", d.cfg.FilterSubjects)

	ticker := time.NewTicker(consumerCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cc.Drain()
			waitClosed(closed, drainTimeout)
			waitGroup(&inflight, drainTimeout)
			return nil
		case <-closed:
			return fmt.Errorf("consumer %s stopped unexpectedly (durable or stream deleted?)", d.cfg.Name)
		case <-suspect:
		case <-ticker.C:
		}
		if err := durableLost(ctx, cons); err != nil {
			cc.Stop()
			waitClosed(closed, drainTimeout)
			return fmt.Errorf("consumer %s lost: %w", d.cfg.Name, err)
		}
	}
}

func waitClosed(closed <-chan struct{}, timeout time.Duration) {
	select {
	case <-closed:
	case <-time.After(timeout):
	}
}

func waitGroup(wg *sync.WaitGroup, timeout time.Duration) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("in-flight handlers still running at shutdown; their messages will be redelivered")
	}
}

func (d *Durable) process(ctx context.Context, msg jetstream.Msg, dlq msgPublisher) {
	hdr := msg.Headers()
	if hdr == nil {
		hdr = nats.Header{}
	}
	ctx = otel.GetTextMapPropagator().Extract(ctx, HeaderCarrier(hdr))
	ctx, span := otel.Tracer(tracerName).Start(ctx, msg.Subject()+" process",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "nats"),
			attribute.String("messaging.destination.name", msg.Subject()),
			attribute.String("messaging.consumer.group.name", d.cfg.Name),
		))
	defer span.End()
	hctx, cancel := context.WithTimeout(ctx, d.cfg.HandleTimeout)
	defer cancel()

	err := safeHandle(hctx, d.handle, msg)
	if err == nil {
		if ackErr := msg.Ack(); ackErr != nil {
			slog.ErrorContext(ctx, "ack failed", "durable", d.cfg.Name, "subject", msg.Subject(), "err", ackErr)
		}
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	var delivered uint64
	if meta, mErr := msg.Metadata(); mErr == nil {
		delivered = meta.NumDelivered
	}
	if !errors.Is(err, ErrPermanent) && delivered < uint64(d.cfg.MaxDeliver) {
		slog.WarnContext(ctx, "handler failed, will retry", "durable", d.cfg.Name, "subject", msg.Subject(),
			"delivered", delivered, "err", err)
		if nakErr := msg.NakWithDelay(d.cfg.NakDelay); nakErr != nil {
			slog.ErrorContext(ctx, "nak failed", "durable", d.cfg.Name, "err", nakErr)
		}
		return
	}
	slog.ErrorContext(ctx, "dead-lettering message", "durable", d.cfg.Name, "subject", msg.Subject(),
		"delivered", delivered, "permanent", errors.Is(err, ErrPermanent), "err", err)
	deadLetterTo(ctx, msg, hdr, dlq, d.cfg.DLQSubject)
}

// safeHandle turns a handler panic into a retryable error, so one bad message
// cannot take the consumer down.
func safeHandle(ctx context.Context, h MsgHandler, msg jetstream.Msg) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("handler panic: %v", r)
		}
	}()
	return h(ctx, msg)
}

// deadLetterTo publishes the raw message to subject and always terminates it.
// The DLQ Nats-Msg-Id derives from the original id (or the payload hash), so
// a replay dedupes inside the DLQ stream's duplicate window.
func deadLetterTo(ctx context.Context, msg jetstream.Msg, hdr nats.Header, dlq msgPublisher, subject string) {
	out := &nats.Msg{Subject: subject, Data: msg.Data(), Header: nats.Header{}}
	for k, v := range hdr {
		if strings.HasPrefix(k, "Nats-") {
			continue
		}
		out.Header[k] = append([]string(nil), v...)
	}
	out.Header.Set(jetstream.MsgIDHeader, DLQMsgID(subject, hdr.Get(jetstream.MsgIDHeader), msg.Data()))
	pubCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := dlq.PublishMsg(pubCtx, out); err != nil {
		slog.ErrorContext(ctx, "dead-letter publish failed; terminating anyway", "subject", subject, "err", err)
	}
	if err := msg.Term(); err != nil {
		slog.ErrorContext(ctx, "term failed", "err", err)
	}
}

// DLQMsgID is the dead letter's Nats-Msg-Id: "<dlq subject>:<original id>",
// or "<dlq subject>:sha256:<hex>" when the original had no id.
func DLQMsgID(subject, originalID string, data []byte) string {
	if originalID != "" {
		return subject + ":" + originalID
	}
	sum := sha256.Sum256(data)
	return subject + ":sha256:" + hex.EncodeToString(sum[:])
}
