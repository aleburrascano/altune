package events

import (
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
}

var _ Bus = (*InProcessBus)(nil)

func NewInProcessBus() *InProcessBus {
	return &InProcessBus{ringCap: defaultRingSize}
}

func (b *InProcessBus) getOrCreateUser(userId shared.UserId) *userState {
	key := userId.String()
	if v, ok := b.users.Load(key); ok {
		return v.(*userState)
	}
	us := &userState{
		ring:        make([]Event, b.ringCap),
		subscribers: make(map[uint64]chan Event),
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
		}
	}
}

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
