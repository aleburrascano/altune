package goapi

import (
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

const (
	racedQueueCap = 4
	racedRounds   = 5000
	racedBurst    = 20000
	racedStagger  = 64
)

func newRacedConsumer() *Consumer {
	c := &Consumer{}
	c.events.pending = make(chan Event, racedQueueCap)
	return c
}

func sequenced(n int) Event { return Event{Type: "raced", Subject: strconv.Itoa(n)} }

func drainFromBucket(c *Consumer) []Event { return DrainPending(c, c.Events()) }

func TestConcurrentDrainAgainstASaturatedBufferLosesOnlyWhatItDropped(t *testing.T) {
	c := newRacedConsumer()
	received := 0
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				received += len(drainFromBucket(c))
			}
		}
	}()

	for sent := range racedBurst {
		c.events.push(sequenced(sent))
	}
	close(stop)
	wg.Wait()
	received += len(drainFromBucket(c))

	if got := received + c.Dropped(); got != racedBurst {
		t.Fatalf("received %d + dropped %d = %d, want every one of the %d sent", received, c.Dropped(), got, racedBurst)
	}
}

func TestDrainRacingAFullBufferNeverCountsADropThatHadRoom(t *testing.T) {
	c := newRacedConsumer()
	sent := 0
	for sent < racedQueueCap {
		c.events.push(sequenced(sent))
		sent++
	}

	for round := range racedRounds {
		head := sequenced(sent - racedQueueCap)
		dropsBefore := c.Dropped()
		var ready atomic.Int32
		var drained []Event
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
			drained = drainFromBucket(c)
		}()
		ready.Add(1)
		for ready.Load() < 2 {
		}
		c.events.push(sequenced(sent))
		sent++
		wg.Wait()

		dropped := c.Dropped() - dropsBefore
		if dropped > 1 {
			t.Fatalf("round %d: one push into a full buffer counted %d drops", round, dropped)
		}
		if dropped == 1 && len(drained) > 0 && drained[0] == head {
			t.Fatalf("round %d: the drainer made room by taking %s, yet the push still evicted and counted a drop", round, head.Subject)
		}
		drainFromBucket(c)
		for len(c.Events()) < racedQueueCap {
			c.events.push(sequenced(sent))
			sent++
		}
	}
}
