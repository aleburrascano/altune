package logging

import (
	"context"
	"log/slog"
	"sync"
	"testing"
)

func newCaptureLogger(t *testing.T, capacity int) (*slog.Logger, *RingBuffer) {
	t.Helper()
	ring := NewRingBuffer(capacity)
	// Discard inner output; we only assert on the captured ring.
	inner := slog.NewJSONHandler(discardWriter{}, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(newRingHandler(inner, ring)), ring
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestRingHandler_CapturesAndGroupsByCorrID(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

	logger.Info("request.start", "corr_id", "abc123", "path", "/x")
	logger.Info("request.complete", "corr_id", "abc123", "status", 200)
	logger.Info("other", "corr_id", "zzz999")

	snap := ring.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("snapshot len = %d, want 3", len(snap)) // AE3 grouping data
	}

	var abc int
	for _, rec := range snap {
		if rec.Attrs["corr_id"] == "abc123" {
			abc++
		}
	}
	if abc != 2 {
		t.Errorf("records with corr_id abc123 = %d, want 2", abc)
	}
}

func TestRingHandler_WithAttrsSharesRing(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

	// A derived logger (per-request child) must feed the same ring.
	child := logger.With("corr_id", "derived")
	child.Info("from child")

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["corr_id"] != "derived" {
		t.Fatalf("derived logger did not reach the shared ring: %+v", snap)
	}
}

func TestRingBuffer_EvictsOldest(t *testing.T) {
	ring := NewRingBuffer(3)
	for i, msg := range []string{"a", "b", "c", "d", "e"} {
		_ = i
		ring.append(CapturedRecord{Message: msg})
	}
	snap := ring.Snapshot()
	if len(snap) != 3 {
		t.Fatalf("len = %d, want 3", len(snap))
	}
	got := []string{snap[0].Message, snap[1].Message, snap[2].Message}
	want := []string{"c", "d", "e"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot = %v, want %v (newest retained, oldest first)", got, want)
		}
	}
}

func TestRingBuffer_Subscribe(t *testing.T) {
	ring := NewRingBuffer(10)
	ch, cancel := ring.Subscribe()
	defer cancel()

	ring.append(CapturedRecord{Message: "live"})

	select {
	case rec := <-ch:
		if rec.Message != "live" {
			t.Fatalf("got %q, want live", rec.Message)
		}
	default:
		t.Fatal("subscriber did not receive the appended record")
	}
}

func TestRingBuffer_SlowSubscriberDropsNotBlocks(t *testing.T) {
	ring := NewRingBuffer(10)
	_, cancel := ring.Subscribe() // never drained
	defer cancel()

	// Far more than the subscriber buffer; must not block the producer.
	done := make(chan struct{})
	go func() {
		for i := 0; i < subscriberChanSize*4; i++ {
			ring.append(CapturedRecord{Message: "x"})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-context.Background().Done():
	}
	// If we reached here without deadlock, the producer was never blocked.
}

func TestRingBuffer_ConcurrentAppends(t *testing.T) {
	ring := NewRingBuffer(100)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				ring.append(CapturedRecord{Message: "c"})
			}
		}()
	}
	wg.Wait()
	if got := len(ring.Snapshot()); got != 100 {
		t.Fatalf("snapshot len = %d, want 100 (capacity)", got)
	}
}
