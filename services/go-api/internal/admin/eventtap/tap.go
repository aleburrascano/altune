package eventtap

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
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

// Tap decorates an events.Publisher, copying every event published through it
// onto one system-wide channel for the admin feed. It is safe for concurrent
// use: Publish and SubscribeAll take mu, and dropped is atomic.
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

func (t *Tap) Publish(ctx context.Context, userId shared.UserId, eventType string, payload map[string]any) {
	t.inner.Publish(ctx, userId, eventType, payload)

	t.mu.Lock()
	if t.ch != nil {
		select {
		case t.ch <- TapEvent{Type: eventType, Timestamp: time.Now().UTC(), User: userId.String(), Subject: tapSubject(payload), CorrID: logging.CorrelationIDFromContext(ctx)}:
		default:
			t.dropped.Add(1)
		}
	}
	t.mu.Unlock()
}

// Emit copies eventType onto the tap channel exactly like Publish does, but
// never reaches the wrapped inner publisher: it carries no user id and no
// payload, so a caller with nothing client-facing to publish (discovery's
// server-side interaction telemetry) can still surface activity on the admin
// event feed without that activity ever entering a mobile client's own event
// stream, and without a user id or free text riding along (#2594, honouring
// the masking from #2585).
func (t *Tap) Emit(eventType string) {
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

func tapSubject(payload map[string]any) string {
	for _, key := range []string{"track_id", "entity_id", "result_signature"} {
		if v, ok := payload[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// SubscribeAll opens the one system-wide subscription this tap allows: while it
// is live a second call fails rather than splitting the stream, so the single
// admin Feed owns it. The returned cancel closes the channel and frees the
// slot; until then a Publish that finds the channel full drops the event and
// counts it in Dropped rather than blocking the publisher.
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
