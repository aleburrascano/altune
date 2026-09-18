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
	userIdleTTL        = 30 * time.Minute
)

type userState struct {
	mu          sync.RWMutex
	ring        []Event
	ringHead    int
	ringLen     int
	nextID      uint64
	subscribers map[uint64]chan Event
	subCounter  uint64
	lastActive  time.Time
	evicted     bool
}

func (us *userState) evictIfReclaimable(cutoff time.Time, removeFromMap func()) {
	us.mu.Lock()
	defer us.mu.Unlock()
	if len(us.subscribers) > 0 || !us.lastActive.Before(cutoff) {
		return
	}
	us.evicted = true
	removeFromMap()
}

type InProcessBus struct {
	users   sync.Map
	ringCap int
	// highestIssuedID is the largest event ID issued to any user in this
	// process. A new or evict-recreated user's sequence starts above it, so an
	// ID is never reused for a user and a pre-eviction afterID stays below
	// every post-eviction event. idFloor carries the same guarantee across a
	// process restart.
	highestIssuedID atomic.Uint64
	dropped         atomic.Uint64
	now             func() time.Time
	idFloor         *idFloor

	beforeEvictDelete func(key string)
}

func (b *InProcessBus) Dropped() uint64 { return b.dropped.Load() }

// HighestIssuedID returns the process-wide event ID high-water mark: the
// largest ID issued so far, or the startup seed if none has been.
func (b *InProcessBus) HighestIssuedID() uint64 { return b.highestIssuedID.Load() }

var (
	_ Publisher  = (*InProcessBus)(nil)
	_ Subscriber = (*InProcessBus)(nil)
)

func NewInProcessBus() *InProcessBus {
	return newBus(time.Now, defaultIDFloorPath())
}

func newBus(now func() time.Time, idFloorPath string) *InProcessBus {
	floor := newIDFloor(idFloorPath)
	b := &InProcessBus{ringCap: defaultRingSize, now: now, idFloor: floor}
	b.highestIssuedID.Store(floor.reserveAbove(uint64(now().UnixNano())))
	return b
}

func (b *InProcessBus) getOrCreateUser(userId shared.UserId) *userState {
	key := userId.String()
	if v, ok := b.users.Load(key); ok {
		return v.(*userState)
	}
	b.evictIdleUsers()
	us := &userState{
		ring:        make([]Event, b.ringCap),
		subscribers: make(map[uint64]chan Event),
		nextID:      b.highestIssuedID.Load(),
		lastActive:  b.now(),
	}
	actual, _ := b.users.LoadOrStore(key, us)
	return actual.(*userState)
}

func (b *InProcessBus) evictIdleUsers() {
	cutoff := b.now().Add(-userIdleTTL)
	b.users.Range(func(key, value any) bool {
		us := value.(*userState)
		us.evictIfReclaimable(cutoff, func() {
			if b.beforeEvictDelete != nil {
				b.beforeEvictDelete(key.(string))
			}
			b.users.CompareAndDelete(key, us)
		})
		return true
	})
}

func (b *InProcessBus) lockLiveUser(userId shared.UserId) *userState {
	for {
		us := b.getOrCreateUser(userId)
		us.mu.Lock()
		if !us.evicted {
			return us
		}
		us.mu.Unlock()
	}
}

func (b *InProcessBus) Publish(userId shared.UserId, eventType string, payload map[string]any) {
	us := b.lockLiveUser(userId)

	us.lastActive = b.now()
	us.nextID++
	b.recordIssuedID(us.nextID)
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

func (b *InProcessBus) recordIssuedID(id uint64) {
	for {
		cur := b.highestIssuedID.Load()
		if id <= cur {
			return
		}
		if b.highestIssuedID.CompareAndSwap(cur, id) {
			b.idFloor.reserveAbove(id)
			return
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
	ch := make(chan Event, subscriberChanSize)

	us := b.lockLiveUser(userId)
	us.subCounter++
	id := us.subCounter
	us.subscribers[id] = ch
	us.mu.Unlock()

	cancel := func() {
		us.mu.Lock()
		delete(us.subscribers, id)
		us.lastActive = b.now()
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
