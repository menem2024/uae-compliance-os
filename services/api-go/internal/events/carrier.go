package events

import "github.com/nats-io/nats.go"

// HeaderCarrier adapts nats.Header to propagation.TextMapCarrier.
type HeaderCarrier nats.Header

// Get returns the first value for key.
func (c HeaderCarrier) Get(key string) string { return nats.Header(c).Get(key) }

// Set replaces the value for key.
func (c HeaderCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }

// Keys lists the header keys.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
