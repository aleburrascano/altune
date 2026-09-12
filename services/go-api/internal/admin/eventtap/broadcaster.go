package eventtap

import "sync"

type broadcaster struct {
	mu      sync.Mutex
	subs    map[int]chan TapEvent
	nextSub int
}

func newBroadcaster() *broadcaster {
	return &broadcaster{subs: make(map[int]chan TapEvent)}
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

func (b *broadcaster) subscribe() (<-chan TapEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextSub
	b.nextSub++
	ch := make(chan TapEvent, feedSubSize)
	b.subs[id] = ch
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if c, ok := b.subs[id]; ok {
			delete(b.subs, id)
			close(c)
		}
	}
}
