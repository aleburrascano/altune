package events

import (
	"altune/go-api/internal/shared/events/internal/core/idfloor"
	"altune/go-api/internal/shared/ids"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
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
	firstID     uint64
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
	users           sync.Map
	ringCap         int
	highestIssuedID atomic.Uint64
	dropped         atomic.Uint64
	now             func() time.Time
	idFloor         *idfloor.Floor
}

func (b *InProcessBus) Dropped() uint64 { return b.dropped.Load() }

func (b *InProcessBus) HighestIssuedID() uint64 { return b.highestIssuedID.Load() }

var _ Publisher = (*InProcessBus)(nil)

func NewInProcessBus(now func() time.Time, idFloorPath string) *InProcessBus {
	floor := idfloor.New(idFloorPath)
	b := &InProcessBus{ringCap: defaultRingSize, now: now, idFloor: floor}
	b.highestIssuedID.Store(floor.ReserveAbove(uint64(now().UnixNano())))
	return b
}

func (b *InProcessBus) getOrCreateUser(userId ids.UserId) *userState {
	key := userId.String()
	if v, ok := b.users.Load(key); ok {
		return v.(*userState)
	}
	b.evictIdleUsers()
	us := &userState{
		ring:        make([]Event, b.ringCap),
		subscribers: make(map[uint64]chan Event),
		firstID:     b.highestIssuedID.Load(),
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
		us.evictIfReclaimable(cutoff, func() { b.users.CompareAndDelete(key, us) })
		return true
	})
}

func (b *InProcessBus) lockLiveUser(userId ids.UserId) *userState {
	for {
		us := b.getOrCreateUser(userId)
		us.mu.Lock()
		if !us.evicted {
			return us
		}
		us.mu.Unlock()
	}
}

func (b *InProcessBus) Publish(_ context.Context, userId ids.UserId, eventType EventType, payload map[string]any) {
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

	b.appendToRing(us, evt)
	b.fanOut(us, userId, evt)
	us.mu.Unlock()
}

func (b *InProcessBus) appendToRing(us *userState, evt Event) {
	us.ring[us.ringHead] = evt
	us.ringHead = (us.ringHead + 1) % b.ringCap
	if us.ringLen < b.ringCap {
		us.ringLen++
	}
}

func (b *InProcessBus) fanOut(us *userState, userId ids.UserId, evt Event) {
	for _, ch := range us.subscribers {
		select {
		case ch <- evt:
		default:
			b.recordDropForFullSubscriber(userId, evt.Type, evt.ID)
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
			b.idFloor.ReserveAbove(id)
			return
		}
	}
}

func (b *InProcessBus) recordDropForFullSubscriber(userId ids.UserId, eventType EventType, eventID uint64) {
	total := b.dropped.Add(1)
	slog.WarnContext(context.Background(), "events.subscriber_dropped",
		"user_id", userId.String(), "event_type", eventType,
		"event_id", eventID, "dropped_total", total)
}

func (b *InProcessBus) Subscribe(userId ids.UserId) (<-chan Event, func()) {
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

func (b *InProcessBus) LatestID(userId ids.UserId) uint64 {
	v, ok := b.users.Load(userId.String())
	if !ok {
		return 0
	}
	us := v.(*userState)
	us.mu.RLock()
	defer us.mu.RUnlock()
	return us.nextID
}

func (b *InProcessBus) ResumeGapped(userId ids.UserId, afterID uint64) bool {
	if afterID == 0 {
		return false
	}
	v, ok := b.users.Load(userId.String())
	if !ok {
		return true
	}
	us := v.(*userState)
	us.mu.RLock()
	defer us.mu.RUnlock()
	return afterID < us.firstID
}

func (b *InProcessBus) Replay(userId ids.UserId, afterID uint64) []Event {
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

func warnIfEventsWereEvictedBeforeResume(userId ids.UserId, afterID, oldestRetainedID uint64) {
	resumingFromBeginning := afterID == 0
	if resumingFromBeginning || oldestRetainedID <= afterID+1 {
		return
	}
	slog.WarnContext(context.Background(), "events.replay_gap",
		"user_id", userId.String(), "after_id", afterID,
		"oldest_retained_id", oldestRetainedID, "lost", oldestRetainedID-afterID-1)
}
