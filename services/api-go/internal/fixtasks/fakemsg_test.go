package fixtasks

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// fakeMsg is a jetstream.Msg for handler tests that never ack.
type fakeMsg struct {
	subject string
	data    []byte
}

func (m fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	return &jetstream.MsgMetadata{NumDelivered: 1, Timestamp: time.Now()}, nil
}
func (m fakeMsg) Data() []byte                     { return m.data }
func (m fakeMsg) Headers() nats.Header             { return nats.Header{} }
func (m fakeMsg) Subject() string                  { return m.subject }
func (m fakeMsg) Reply() string                    { return "" }
func (m fakeMsg) Ack() error                       { return nil }
func (m fakeMsg) DoubleAck(context.Context) error  { return nil }
func (m fakeMsg) Nak() error                       { return nil }
func (m fakeMsg) NakWithDelay(time.Duration) error { return nil }
func (m fakeMsg) InProgress() error                { return nil }
func (m fakeMsg) Term() error                      { return nil }
func (m fakeMsg) TermWithReason(string) error      { return nil }
