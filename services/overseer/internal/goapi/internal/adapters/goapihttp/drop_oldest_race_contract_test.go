package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	racedQueueCap = 4
	racedRounds   = 5000
	racedStagger  = 64
)

func sequenced(n int) goapi.Event { return goapi.Event{Type: "raced", Subject: strconv.Itoa(n)} }

func TestDrainRacingAFullBufferNeverCountsADropThatHadRoom(t *testing.T) {
	q := goapihttp.NewDropOldestQueue[goapi.Event](racedQueueCap)
	sent := 0
	for sent < racedQueueCap {
		q.Push(sequenced(sent))
		sent++
	}

	for round := range racedRounds {
		head := sequenced(sent - racedQueueCap)
		dropsBefore := q.Dropped()
		var ready atomic.Int32
		var drained []goapi.Event
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			ready.Add(1)
			for ready.Load() < 2 {
			}
			for range round % racedStagger {
				ready.Load()
			}
			drained = q.Drain()
		}()
		ready.Add(1)
		for ready.Load() < 2 {
		}
		q.Push(sequenced(sent))
		sent++
		wg.Wait()

		dropped := q.Dropped() - dropsBefore
		if dropped > 1 {
			t.Fatalf("round %d: one push into a full buffer counted %d drops", round, dropped)
		}
		if dropped == 1 && len(drained) > 0 && drained[0] == head {
			t.Fatalf("round %d: the drainer made room by taking %s, yet the push still evicted and counted a drop", round, head.Subject)
		}
		q.Drain()
		for q.Len() < racedQueueCap {
			q.Push(sequenced(sent))
			sent++
		}
	}
}
