package service

import (
	"altune/go-api/internal/shared/clock"
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

type EventTap struct {
	inner events.Publisher
	now   clock.Func

	mu      sync.Mutex
	ch      chan TapEvent
	dropped atomic.Uint64
}

var _ events.Publisher = (*EventTap)(nil)

var ErrTapClockRequired = errors.New("eventwatch: the tap requires a clock")

func NewEventTap(inner events.Publisher, now clock.Func) (*EventTap, error) {
	if now == nil {
		return nil, ErrTapClockRequired
	}
	return &EventTap{inner: inner, now: now}, nil
}

func MustNewEventTap(inner events.Publisher, now clock.Func) *EventTap {
	t, err := NewEventTap(inner, now)
	if err != nil {
		panic(err)
	}
	return t
}

func (t *EventTap) Publish(ctx context.Context, userId ids.UserId, payload events.Payload) {
	t.inner.Publish(ctx, userId, payload)

	t.mu.Lock()
	if t.ch != nil {
		select {
		case t.ch <- TapEvent{Type: string(payload.EventType()), Timestamp: t.now().UTC(), User: userId.String(), Subject: tapSubject(payload), CorrID: logging.CorrelationIDFromContext(ctx)}:
		default:
			t.dropped.Add(1)
		}
	}
	t.mu.Unlock()
}

func (t *EventTap) EmitActivity(eventType string) {
	t.mu.Lock()
	if t.ch != nil {
		select {
		case t.ch <- TapEvent{Type: eventType, Timestamp: t.now().UTC()}:
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

func (t *EventTap) SubscribeAll() (<-chan TapEvent, func(), error) {
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

func (t *EventTap) Dropped() uint64 { return t.dropped.Load() }
