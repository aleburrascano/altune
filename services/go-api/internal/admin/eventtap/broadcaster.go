package eventtap

import (
	"errors"
	"sync"
)

// MaxSubscribers bounds concurrent live subscribers to the admin event feed.
// The feed is operator-only, so a handful of open dashboards is the real load;
// the ceiling exists so a leaked token or reconnect storm cannot grow the
// subscriber set (one goroutine and buffered channel each) without limit.
const MaxSubscribers = 16

// ErrTooManySubscribers is returned by Subscribe once MaxSubscribers are live.
var ErrTooManySubscribers = errors.New("eventtap: too many feed subscribers")

// broadcaster fans one feed's events out to the live console subscribers. It is
// safe for concurrent use: every path takes mu, so the feed's loop goroutine
// broadcasts while request goroutines subscribe and unsubscribe.
type broadcaster struct {
	mu      sync.Mutex
	subs    map[int]chan TapEvent
	nextSub int
	maxSubs int
}

func newBroadcaster(maxSubs int) *broadcaster {
	return &broadcaster{subs: make(map[int]chan TapEvent), maxSubs: maxSubs}
}

func (b *broadcaster) broadcast(evt TapEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- evt:
		default:
		}
	}
}

// subscribe registers a live subscriber, or returns ErrTooManySubscribers
// without registering one when the ceiling is already reached.
func (b *broadcaster) subscribe() (<-chan TapEvent, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.subs) >= b.maxSubs {
		return nil, nil, ErrTooManySubscribers
	}
	id := b.nextSub
	b.nextSub++
	ch := make(chan TapEvent, feedSubSize)
	b.subs[id] = ch
	return ch, func() { b.unsubscribe(id) }, nil
}

func (b *broadcaster) unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if c, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(c)
	}
}
