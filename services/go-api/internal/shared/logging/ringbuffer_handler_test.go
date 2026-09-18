package logging

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

func newCaptureLogger(t *testing.T, capacity int) (*slog.Logger, *RingBuffer) {
	t.Helper()
	ring := NewRingBuffer(capacity)
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
		t.Fatalf("snapshot len = %d, want 3", len(snap))
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

func TestRingHandler_RedactsQueryTextAtTheBoundary(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

	const secret = "taylorswiftsecretdiary"
	logger.Info("search.v2.start", "corr_id", "abc123", "query", secret)

	snap := ring.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	if v, ok := snap[0].Attrs["query"]; ok {
		t.Errorf("query attr reached the ring: %q", v)
	}
	for k, v := range snap[0].Attrs {
		if strings.Contains(v, secret) {
			t.Errorf("raw query text leaked into ring attr %q = %q", k, v)
		}
	}
	if snap[0].Attrs["corr_id"] != "abc123" {
		t.Errorf("non-sensitive attr dropped by redaction: corr_id = %q, want abc123", snap[0].Attrs["corr_id"])
	}
}

func TestRingHandler_WithAttrsSharesRing(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

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
	ch, cancel, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
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
	_, cancelNeverDrainedSubscriber, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancelNeverDrainedSubscriber()

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
}

// TestRingBuffer_CountsRecordsDroppedForAFullSubscriber pins #1607: records a
// full subscriber channel discards used to vanish with no trace, so an
// operator's live log view could lose the lines diagnosing an incident without
// anything saying so.
func TestRingBuffer_CountsRecordsDroppedForAFullSubscriber(t *testing.T) {
	ring := NewRingBuffer(10)
	_, cancelNeverDrainedSubscriber, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancelNeverDrainedSubscriber()

	const appendsPastChanCapacity = 5
	for i := 0; i < subscriberChanSize+appendsPastChanCapacity; i++ {
		ring.append(CapturedRecord{Message: "burst"})
	}

	if got := ring.Dropped(); got != appendsPastChanCapacity {
		t.Fatalf("Dropped() = %d, want %d (appends past the subscriber's %d-slot channel)",
			got, appendsPastChanCapacity, subscriberChanSize)
	}
}

func TestRingBuffer_CountsNoDropWhileTheSubscriberKeepsUp(t *testing.T) {
	ring := NewRingBuffer(10)
	ch, cancel, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	for i := 0; i < subscriberChanSize*2; i++ {
		ring.append(CapturedRecord{Message: "drained"})
		<-ch
	}

	if got := ring.Dropped(); got != 0 {
		t.Fatalf("Dropped() = %d, want 0 (every record was delivered)", got)
	}
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

// TestRingBuffer_SubscribeRejectsPastCeiling pins #996: once MaxSubscribers are
// live, Subscribe refuses the next one without disturbing existing subscribers,
// and cancelling a subscription frees its slot.
func TestRingBuffer_SubscribeRejectsPastCeiling(t *testing.T) {
	ring := NewRingBuffer(10)
	chans := make([]<-chan CapturedRecord, 0, MaxSubscribers)
	cancels := make([]func(), 0, MaxSubscribers)
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()
	for i := 0; i < MaxSubscribers; i++ {
		ch, cancel, err := ring.Subscribe()
		if err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
		chans = append(chans, ch)
		cancels = append(cancels, cancel)
	}

	if ch, cancel, err := ring.Subscribe(); !errors.Is(err, ErrTooManySubscribers) || ch != nil || cancel != nil {
		t.Fatalf("subscribe past ceiling = (%v, %v, %v), want ErrTooManySubscribers and no channel", ch, cancel != nil, err)
	}

	ring.append(CapturedRecord{Message: "still-live"})
	for i, ch := range chans {
		select {
		case rec := <-ch:
			if rec.Message != "still-live" {
				t.Fatalf("subscriber %d got %q, want still-live", i+1, rec.Message)
			}
		default:
			t.Fatalf("existing subscriber %d stopped receiving after a rejection", i+1)
		}
	}

	cancels[0]()
	cancels[0] = func() {}
	_, cancel, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("subscribe after a cancel freed a slot: %v", err)
	}
	cancels = append(cancels, cancel)
}
