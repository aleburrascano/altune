package eventwatch

import (
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

const tapChanSize = 256

type TapEvent struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	User      string    `json:"user,omitempty"`
	Subject   string    `json:"subject,omitempty"`
	CorrID    string    `json:"corr_id,omitempty"`
}

type EventWatcher struct {
	inner events.Publisher

	mu      sync.Mutex
	ch      chan TapEvent
	dropped atomic.Uint64
}

var _ events.Publisher = (*EventWatcher)(nil)

func New(inner events.Publisher) *EventWatcher {
	return &EventWatcher{inner: inner}
}

func (t *EventWatcher) Publish(ctx context.Context, userId ids.UserId, payload events.Payload) {
	t.inner.Publish(ctx, userId, payload)

	t.mu.Lock()
	if t.ch != nil {
		select {
		case t.ch <- TapEvent{Type: string(payload.EventType()), Timestamp: time.Now().UTC(), User: userId.String(), Subject: tapSubject(payload), CorrID: logging.CorrelationIDFromContext(ctx)}:
		default:
			t.dropped.Add(1)
		}
	}
	t.mu.Unlock()
}

func (t *EventWatcher) EmitActivity(eventType string) {
	t.mu.Lock()
	if t.ch != nil {
		select {
		case t.ch <- TapEvent{Type: eventType, Timestamp: time.Now().UTC()}:
		default:
			t.dropped.Add(1)
		}
	}
	t.mu.Unlock()
}

func tapSubject(payload events.Payload) string {
	if s, ok := payload.(events.Subjected); ok {
		return s.Subject()
	}
	return ""
}

func (t *EventWatcher) SubscribeAll() (<-chan TapEvent, func(), error) {
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

func (t *EventWatcher) Dropped() uint64 { return t.dropped.Load() }
