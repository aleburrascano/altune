package events

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"altune/go-api/internal/shared"
)

const (
	defaultRingSize    = 100
	subscriberChanSize = 16
)

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
	idBase  uint64
	dropped atomic.Uint64
}

func (b *InProcessBus) Dropped() uint64 { return b.dropped.Load() }

var (
	_ Publisher  = (*InProcessBus)(nil)
	_ Subscriber = (*InProcessBus)(nil)
)

func NewInProcessBus() *InProcessBus {
	return &InProcessBus{ringCap: defaultRingSize, idBase: idBaseMonotonicAcrossRestarts()}
}

func idBaseMonotonicAcrossRestarts() uint64 {
	return uint64(time.Now().UnixNano())
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
			b.recordDropForFullSubscriber(userId, eventType, evt.ID)
		}
	}
}

func (b *InProcessBus) recordDropForFullSubscriber(userId shared.UserId, eventType string, eventID uint64) {
	total := b.dropped.Add(1)
	slog.Warn("events.subscriber_dropped",
		"user_id", userId.String(), "event_type", eventType,
		"event_id", eventID, "dropped_total", total)
}

func (b *InProcessBus) Subscribe(userId shared.UserId) (<-chan Event, func()) {
	us := b.getOrCreateUser(userId)
	ch := make(chan Event, subscriberChanSize)

	us.mu.Lock()
	us.subCounter++
	id := us.subCounter
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

	warnIfEventsWereEvictedBeforeResume(userId, afterID, us.ring[start].ID)

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

func warnIfEventsWereEvictedBeforeResume(userId shared.UserId, afterID, oldestRetainedID uint64) {
	resumingFromBeginning := afterID == 0
	if resumingFromBeginning || oldestRetainedID <= afterID+1 {
		return
	}
	slog.Warn("events.replay_gap",
		"user_id", userId.String(), "after_id", afterID,
		"oldest_retained_id", oldestRetainedID, "lost", oldestRetainedID-afterID-1)
}
