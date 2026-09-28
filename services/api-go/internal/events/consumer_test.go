package events

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	compliancev1 "github.com/menem2024/uae-platform/services/api-go/gen/compliance/v1"
)

type fakeMsg struct {
	jetstream.Msg
	data      []byte
	hdr       nats.Header
	delivered uint64
	acked     bool
	termed    bool
	nakDelay  time.Duration
}

func (m *fakeMsg) Data() []byte         { return m.data }
func (m *fakeMsg) Headers() nats.Header { return m.hdr }
func (m *fakeMsg) Subject() string      { return ExtractedSubject }
func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: m.delivered}, nil
}
func (m *fakeMsg) Ack() error                         { m.acked = true; return nil }
func (m *fakeMsg) Term() error                        { m.termed = true; return nil }
func (m *fakeMsg) NakWithDelay(d time.Duration) error { m.nakDelay = d; return nil }

type fakeDLQ struct {
	fail bool
	got  []*nats.Msg
}

func (d *fakeDLQ) PublishMsg(_ context.Context, m *nats.Msg, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	d.got = append(d.got, m)
	if d.fail {
		return nil, errors.New("nats down")
	}
	return &jetstream.PubAck{}, nil
}

func newMsg(t *testing.T, delivered uint64) *fakeMsg {
	t.Helper()
	b, err := proto.Marshal(&compliancev1.InvoiceExtracted{InvoiceId: "i1", FirmId: "f1", Invoice: &compliancev1.Invoice{}})
	if err != nil {
		t.Fatal(err)
	}
	return &fakeMsg{data: b, hdr: nats.Header{}, delivered: delivered}
}

func TestProcessMessage(t *testing.T) {
	ctx := context.Background()
	transient := errors.New("validator down")
	permanent := fmt.Errorf("%w: invoice hidden", ErrPermanent)

	cases := []struct {
		name      string
		delivered uint64
		handleErr error
		dlqFail   bool
		data      []byte
		wantAck   bool
		wantTerm  bool
		wantNak   time.Duration
		wantDLQ   int
	}{
		{name: "success acks", delivered: 1, wantAck: true},
		{name: "transient naks with delay", delivered: 1, handleErr: transient, wantNak: 2 * time.Second},
		{name: "transient at max deliver dead-letters", delivered: MaxDeliver, handleErr: transient, wantTerm: true, wantDLQ: 1},
		{name: "permanent dead-letters immediately", delivered: 1, handleErr: permanent, wantTerm: true, wantDLQ: 1},
		{name: "dlq failure still terms", delivered: 1, handleErr: permanent, dlqFail: true, wantTerm: true, wantDLQ: 1},
		{name: "undecodable payload dead-letters", delivered: 1, data: []byte{0xff, 0xff, 0xff}, wantTerm: true, wantDLQ: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newMsg(t, tc.delivered)
			if tc.data != nil {
				m.data = tc.data
			}
			dlq := &fakeDLQ{fail: tc.dlqFail}
			called := false
			processMessage(ctx, m, dlq, func(context.Context, *compliancev1.InvoiceExtracted) error {
				called = true
				return tc.handleErr
			})
			if m.acked != tc.wantAck || m.termed != tc.wantTerm || m.nakDelay != tc.wantNak || len(dlq.got) != tc.wantDLQ {
				t.Errorf("ack=%v term=%v nak=%v dlq=%d", m.acked, m.termed, m.nakDelay, len(dlq.got))
			}
			if tc.data != nil && called {
				t.Error("handler called for undecodable payload")
			}
			for _, d := range dlq.got {
				if d.Subject != DLQExtractedSubject || string(d.Data) != string(m.data) {
					t.Errorf("dlq msg subject=%s data mismatch", d.Subject)
				}
				if d.Header.Get("Nats-Msg-Id") == "" {
					t.Errorf("dlq msg missing Nats-Msg-Id header: %+v", d.Header)
				}
			}
		})
	}
}

// TestDeadLetterMsgIDIsDeterministic verifies redelivery of the same invoice
// (e.g. after the durable is recreated and the retained stream replays)
// dead-letters with the same Nats-Msg-Id, so JetStream's duplicate window
// can suppress the second DLQ store.
func TestDeadLetterMsgIDIsDeterministic(t *testing.T) {
	ctx := context.Background()
	permanent := fmt.Errorf("%w: invoice hidden", ErrPermanent)
	handle := func(context.Context, *compliancev1.InvoiceExtracted) error { return permanent }

	dlq := &fakeDLQ{}
	processMessage(ctx, newMsg(t, 1), dlq, handle)
	processMessage(ctx, newMsg(t, 2), dlq, handle) // redelivery of the same invoice (i1)
	if len(dlq.got) != 2 {
		t.Fatalf("dlq got %d messages, want 2", len(dlq.got))
	}
	id1, id2 := dlq.got[0].Header.Get("Nats-Msg-Id"), dlq.got[1].Header.Get("Nats-Msg-Id")
	if id1 == "" || id1 != id2 {
		t.Errorf("Nats-Msg-Id not deterministic across redelivery: %q vs %q", id1, id2)
	}

	// A different invoice must get a different id.
	other, err := proto.Marshal(&compliancev1.InvoiceExtracted{InvoiceId: "i2", FirmId: "f1", Invoice: &compliancev1.Invoice{}})
	if err != nil {
		t.Fatal(err)
	}
	m3 := newMsg(t, 1)
	m3.data = other
	processMessage(ctx, m3, dlq, handle)
	if id3 := dlq.got[2].Header.Get("Nats-Msg-Id"); id3 == id1 {
		t.Errorf("different invoices got the same Nats-Msg-Id: %q", id3)
	}
}

// TestDeadLetterMsgIDForUndecodablePayloadIsDeterministic covers the case
// where the InvoiceExtracted payload cannot be decoded, so there is no
// invoice id to key on.
func TestDeadLetterMsgIDForUndecodablePayloadIsDeterministic(t *testing.T) {
	ctx := context.Background()
	handle := func(context.Context, *compliancev1.InvoiceExtracted) error {
		t.Fatal("handler called for undecodable payload")
		return nil
	}
	garbage := []byte{0xff, 0xff, 0xff}

	dlq := &fakeDLQ{}
	m1, m2 := newMsg(t, 1), newMsg(t, 1)
	m1.data, m2.data = garbage, garbage
	processMessage(ctx, m1, dlq, handle)
	processMessage(ctx, m2, dlq, handle)
	if len(dlq.got) != 2 {
		t.Fatalf("dlq got %d messages, want 2", len(dlq.got))
	}
	id1, id2 := dlq.got[0].Header.Get("Nats-Msg-Id"), dlq.got[1].Header.Get("Nats-Msg-Id")
	if id1 == "" || id1 != id2 {
		t.Errorf("Nats-Msg-Id not deterministic for identical undecodable payloads: %q vs %q", id1, id2)
	}
}

func TestProcessMessageContinuesTrace(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

	m := newMsg(t, 1)
	m.hdr.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	var got trace.SpanContext
	processMessage(context.Background(), m, &fakeDLQ{}, func(ctx context.Context, _ *compliancev1.InvoiceExtracted) error {
		got = trace.SpanContextFromContext(ctx)
		return nil
	})
	if got.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" || got.SpanID().String() == "00f067aa0ba902b7" {
		t.Fatalf("handler span = %s/%s, want child of remote parent", got.TraceID(), got.SpanID())
	}
}
