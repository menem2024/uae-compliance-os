package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ErrTooManyStreams is returned by Hub.Subscribe when a Firm already has
// maxPerFirm live subscribers (429 too_many_streams).
var ErrTooManyStreams = errors.New("too many streams")

// Event is one item of the /v1/agents/activity feed: a run, step or proposal
// projection, or a resync marker telling the client its buffer overflowed and
// it must re-read via the backfill endpoints.
type Event struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Firm uuid.UUID       `json:"-"`
	Data json.RawMessage `json:"data,omitempty"`
}

// EventID is the feed's SSE id: field and Event.ID.
func EventID(id string, at time.Time) string {
	return fmt.Sprintf("%d-%s", at.UnixMilli(), id)
}

// Subscriber is one Firm's live connection. The channel never blocks the
// publisher: a full buffer drops the event and replaces the next delivery
// with a resync marker.
type Subscriber struct {
	firmID uuid.UUID
	ch     chan Event
	hub    *Hub

	mu      sync.Mutex
	dropped bool
	closed  bool
}

// Events is the subscriber's event channel; it is closed on Close.
func (s *Subscriber) Events() <-chan Event { return s.ch }

// Close stops delivery to this subscriber and removes it from the Hub. Safe
// to call concurrently with Hub.Publish and more than once.
func (s *Subscriber) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	close(s.ch)
	s.mu.Unlock()
	s.hub.remove(s)
}

func (s *Subscriber) deliver(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.dropped {
		ev = Event{Type: "resync", Firm: s.firmID}
	}
	select {
	case s.ch <- ev:
		s.dropped = false
	default:
		s.dropped = true
	}
}

// Hub fans out agent events to per-Firm SSE subscribers. Firm isolation is
// structural: Publish only ever looks at that Firm's own subscriber set, so
// no code path can hand one Firm's subscriber another Firm's event (AC-5).
type Hub struct {
	mu         sync.Mutex
	maxPerFirm int
	buffer     int
	subs       map[uuid.UUID]map[*Subscriber]struct{}
}

// NewHub returns a Hub capping each Firm at maxPerFirm subscribers, each with
// a buffer-sized event channel.
func NewHub(maxPerFirm, buffer int) *Hub {
	return &Hub{maxPerFirm: maxPerFirm, buffer: buffer, subs: map[uuid.UUID]map[*Subscriber]struct{}{}}
}

// Subscribe returns a new live subscriber for firmID, or ErrTooManyStreams if
// that Firm already has maxPerFirm subscribers.
func (h *Hub) Subscribe(firmID uuid.UUID) (*Subscriber, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[firmID]
	if len(set) >= h.maxPerFirm {
		return nil, ErrTooManyStreams
	}
	s := &Subscriber{firmID: firmID, ch: make(chan Event, h.buffer), hub: h}
	if set == nil {
		set = map[*Subscriber]struct{}{}
		h.subs[firmID] = set
	}
	set[s] = struct{}{}
	return s, nil
}

func (h *Hub) remove(s *Subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.subs[s.firmID]
	delete(set, s)
	if len(set) == 0 {
		delete(h.subs, s.firmID)
	}
}

// Publish delivers ev to every current subscriber of firmID. It never reads
// or writes any other Firm's subscriber set.
func (h *Hub) Publish(firmID uuid.UUID, ev Event) {
	h.mu.Lock()
	set := h.subs[firmID]
	subs := make([]*Subscriber, 0, len(set))
	for s := range set {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	for _, s := range subs {
		s.deliver(ev)
	}
}
