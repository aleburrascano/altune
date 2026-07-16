// Package eventtap decorates the shared event bus with the Mission Control
// system-wide tap. It is admin-owned on purpose: the tap's vocabulary (which
// payload keys make a useful operator subject line) belongs to the console,
// not to internal/shared/events, which every feature imports.
package eventtap

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

const tapChanSize = 256

// TapEvent is one system-wide tap event for the operator console. It carries the
// user and a short subject summary (operator full-visibility — the console is a
// single-operator, auth-gated surface; see the verbosity-rework decision). Still
// lossy and single-consumer per ADR-0012.
type TapEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
}

// Tap is an events.Publisher decorator: it forwards every publish to the inner
// bus unchanged and mirrors a redacted copy to the operator console's tap
// channel. Wired in the composition root; services see only events.Publisher.
type Tap struct {
	inner events.Publisher

	// Single consumer, guarded by mu so a concurrent cancel can't close the
	// channel mid-send.
	mu      sync.Mutex
	ch      chan TapEvent
	dropped atomic.Uint64
}

var _ events.Publisher = (*Tap)(nil)

func New(inner events.Publisher) *Tap {
	return &Tap{inner: inner}
}

// Publish forwards to the inner bus, then mirrors to the tap: type + time +
// user + a short subject (operator full-visibility), non-blocking, single
// consumer. The tiny critical section guards against a concurrent cancel
// closing the channel mid-send; the send itself never blocks.
func (t *Tap) Publish(userId shared.UserId, eventType string, payload map[string]any) {
	t.inner.Publish(userId, eventType, payload)

	t.mu.Lock()
	if t.ch != nil {
		select {
		case t.ch <- TapEvent{Type: eventType, Timestamp: time.Now().UTC(), User: userId.String(), Subject: tapSubject(payload)}:
		default:
			t.dropped.Add(1)
		}
	}
	t.mu.Unlock()
}

// tapSubject extracts a concise human subject from an event payload for the
// operator feed, trying the common identifying keys in order. Empty when none
// are present (the row then shows just type + user).
func tapSubject(payload map[string]any) string {
	for _, key := range []string{"query", "title", "name", "track_id", "entity_id", "result_signature"} {
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// SubscribeAll returns the system-wide tap of every published event (type, time,
// user, and a short subject — operator full-visibility). At most one consumer at
// a time; a second subscribe returns an error. Lossy: a slow consumer drops
// events, consistent with the per-user bus (ADR-0012).
func (t *Tap) SubscribeAll() (<-chan TapEvent, func(), error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.ch != nil {
		return nil, nil, errors.New("eventtap: system-wide tap already has a subscriber")
	}
	ch := make(chan TapEvent, tapChanSize)
	t.ch = ch
	cancel := func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		if t.ch == ch {
			t.ch = nil
			close(ch)
		}
	}
	return ch, cancel, nil
}

// Dropped reports events dropped because the tap consumer was too slow.
func (t *Tap) Dropped() uint64 { return t.dropped.Load() }
