package eventtap

import (
	"errors"
	"sync"
)

const MaxSubscribers = 16

var ErrTooManySubscribers = errors.New("eventtap: too many feed subscribers")

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
