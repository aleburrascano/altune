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

type TapEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
}

type Tap struct {
	inner events.Publisher

	mu      sync.Mutex
	ch      chan TapEvent
	dropped atomic.Uint64
}

var _ events.Publisher = (*Tap)(nil)

func New(inner events.Publisher) *Tap {
	return &Tap{inner: inner}
}

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

func (t *Tap) Dropped() uint64 { return t.dropped.Load() }
