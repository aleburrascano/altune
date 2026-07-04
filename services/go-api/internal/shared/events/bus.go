package events

import (
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"altune/go-api/internal/shared"
)

const (
	defaultRingSize    = 100
	subscriberChanSize = 16
	tapChanSize        = 256
)

// TapEvent is a redacted, system-wide view of a published event for the
// operator console: event type and time only. It deliberately omits user id and
// payload — one user must never see another's activity through the feed.
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

type userState struct {
	mu          sync.RWMutex
	ring        []Event
	ringHead    int
	ringLen     int
	nextID      uint64
	subscribers map[uint64]chan Event
	subCounter  uint64
}

type InProcessBus struct {
	users   sync.Map
	ringCap int
	// idBase seeds every user's monotonic event counter at process start. The
	// per-user nextID resets to 0 on restart otherwise (F5), so a client that
	// had already seen low ids from the previous process would mis-dedupe /
	// stop on reconnect. Seeding from the wall clock makes ids monotonic across
	// restarts: a later process always starts above the earlier one's range.
	idBase  uint64
	dropped atomic.Uint64

	// System-wide tap (operator console). Single consumer, guarded by tapMu so
	// a concurrent cancel can't close the channel mid-send.
	tapMu      sync.Mutex
	tap        chan TapEvent
	tapDropped atomic.Uint64
}

// Dropped reports the total number of events dropped because a subscriber's
// buffer was full — the lossy-by-design backpressure made observable.
func (b *InProcessBus) Dropped() uint64 { return b.dropped.Load() }

var _ Bus = (*InProcessBus)(nil)

func NewInProcessBus() *InProcessBus {
	return &InProcessBus{ringCap: defaultRingSize, idBase: uint64(time.Now().UnixNano())}
}

func (b *InProcessBus) getOrCreateUser(userId shared.UserId) *userState {
	key := userId.String()
	if v, ok := b.users.Load(key); ok {
		return v.(*userState)
	}
	us := &userState{
		ring:        make([]Event, b.ringCap),
		subscribers: make(map[uint64]chan Event),
		nextID:      b.idBase,
	}
	actual, _ := b.users.LoadOrStore(key, us)
	return actual.(*userState)
}

func (b *InProcessBus) Publish(userId shared.UserId, eventType string, payload map[string]any) {
	us := b.getOrCreateUser(userId)
	us.mu.Lock()

	us.nextID++
	evt := Event{
		ID:        us.nextID,
		Type:      eventType,
		UserID:    userId,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}

	us.ring[us.ringHead] = evt
	us.ringHead = (us.ringHead + 1) % b.ringCap
	if us.ringLen < b.ringCap {
		us.ringLen++
	}

	subs := make([]chan Event, 0, len(us.subscribers))
	for _, ch := range us.subscribers {
		subs = append(subs, ch)
	}
	us.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			// Subscriber buffer full: drop (the ring + Replay is the recovery
			// path). Lossy by design, but no longer silent.
			total := b.dropped.Add(1)
			slog.Warn("events.subscriber_dropped",
				"user_id", userId.String(), "event_type", eventType,
				"event_id", evt.ID, "dropped_total", total)
		}
	}

	// System-wide tap: type + time + user + a short subject (operator
	// full-visibility), non-blocking, single consumer. The tiny critical section
	// guards against a concurrent cancel closing the channel mid-send; the send
	// itself never blocks.
	b.tapMu.Lock()
	if b.tap != nil {
		select {
		case b.tap <- TapEvent{Type: eventType, Timestamp: evt.Timestamp, User: userId.String(), Subject: tapSubject(payload)}:
		default:
			b.tapDropped.Add(1)
		}
	}
	b.tapMu.Unlock()
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
func (b *InProcessBus) SubscribeAll() (<-chan TapEvent, func(), error) {
	b.tapMu.Lock()
	defer b.tapMu.Unlock()
	if b.tap != nil {
		return nil, nil, errors.New("events: system-wide tap already has a subscriber")
	}
	ch := make(chan TapEvent, tapChanSize)
	b.tap = ch
	cancel := func() {
		b.tapMu.Lock()
		defer b.tapMu.Unlock()
		if b.tap == ch {
			b.tap = nil
			close(ch)
		}
	}
	return ch, cancel, nil
}

// TapDropped reports events dropped because the tap consumer was too slow.
func (b *InProcessBus) TapDropped() uint64 { return b.tapDropped.Load() }

func (b *InProcessBus) Subscribe(userId shared.UserId) (<-chan Event, func()) {
	us := b.getOrCreateUser(userId)
	ch := make(chan Event, subscriberChanSize)

	us.mu.Lock()
	id := atomic.AddUint64(&us.subCounter, 1)
	us.subscribers[id] = ch
	us.mu.Unlock()

	cancel := func() {
		us.mu.Lock()
		delete(us.subscribers, id)
		us.mu.Unlock()
	}
	return ch, cancel
}

func (b *InProcessBus) Replay(userId shared.UserId, afterID uint64) []Event {
	key := userId.String()
	v, ok := b.users.Load(key)
	if !ok {
		return nil
	}
	us := v.(*userState)

	us.mu.RLock()
	defer us.mu.RUnlock()

	if us.ringLen == 0 {
		return nil
	}

	start := us.ringHead - us.ringLen
	if start < 0 {
		start += b.ringCap
	}

	// Gap detection: if the caller resumes after an id that has already been
	// evicted from the ring, events between afterID and the oldest retained id
	// are lost. The client receives only the retained tail — surface the gap so
	// it is diagnosable (a resume that silently loses events otherwise looks like
	// a clean resume). afterID 0 means "from the beginning" — no gap expected.
	oldestID := us.ring[start].ID
	if afterID > 0 && oldestID > afterID+1 {
		slog.Warn("events.replay_gap",
			"user_id", userId.String(), "after_id", afterID,
			"oldest_retained_id", oldestID, "lost", oldestID-afterID-1)
	}

	var result []Event
	for i := 0; i < us.ringLen; i++ {
		idx := (start + i) % b.ringCap
		evt := us.ring[idx]
		if evt.ID > afterID {
			result = append(result, evt)
		}
	}
	return result
}
