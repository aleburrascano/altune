package logging_test

import (
	"altune/go-api/internal/shared/logging"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	ringCapacity            = 1000
	subscriberBufferSize    = 64
	ringTextCapBytes        = 2048
	appendsPastChanCapacity = 5
)

func newCaptureLogger(t *testing.T) (*slog.Logger, *logging.RingBuffer) {
	t.Helper()
	logger, ring, _ := setupLogger(t, "debug", false)
	return logger, ring
}

func logMessages(logger *slog.Logger, n int) {
	for i := range n {
		logger.Info(fmt.Sprintf("m%d", i))
	}
}

func TestRingHandler_CapturesAndGroupsByCorrID(t *testing.T) {
	logger, ring := newCaptureLogger(t)

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
	logger, ring := newCaptureLogger(t)

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
	logger, ring := newCaptureLogger(t)

	child := logger.With("corr_id", "derived")
	child.Info("from child")

	snap := ring.Snapshot()
	if len(snap) != 1 || snap[0].Attrs["corr_id"] != "derived" {
		t.Fatalf("derived logger did not reach the shared ring: %+v", snap)
	}
}

func TestRingBuffer_EvictsOldest(t *testing.T) {
	logger, ring := newCaptureLogger(t)

	logMessages(logger, ringCapacity+3)

	snap := ring.Snapshot()
	if len(snap) != ringCapacity {
		t.Fatalf("len = %d, want %d", len(snap), ringCapacity)
	}
	if snap[0].Message != "m3" || snap[ringCapacity-1].Message != "m1002" {
		t.Fatalf("snapshot runs %q..%q, want m3..m1002 (newest retained, oldest first)", snap[0].Message, snap[ringCapacity-1].Message)
	}
}

func TestRingBuffer_Subscribe(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	ch, cancel, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	logger.Info("live")

	select {
	case rec := <-ch:
		if rec.Message != "live" {
			t.Fatalf("got %q, want live", rec.Message)
		}
	default:
		t.Fatal("subscriber did not receive the appended record")
	}
}

func TestRingBuffer_CancelClosesTheSubscription(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	ch, cancel, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	cancel()
	cancel()
	logger.Info("after cancel")

	if rec, open := <-ch; open {
		t.Fatalf("cancelled subscription received %+v, want a closed channel", rec)
	}
}

func TestRingBuffer_SlowSubscriberDropsNotBlocks(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	_, cancelNeverDrainedSubscriber, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancelNeverDrainedSubscriber()

	done := make(chan struct{})
	go func() {
		for range subscriberBufferSize * 4 {
			logger.Info("x")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("logging blocked on a subscriber that never drains")
	}
}

func TestRingBuffer_CountsRecordsDroppedForAFullSubscriber(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	_, cancelNeverDrainedSubscriber, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancelNeverDrainedSubscriber()

	for range subscriberBufferSize + appendsPastChanCapacity {
		logger.Info("burst")
	}

	if got := ring.Dropped(); got != appendsPastChanCapacity {
		t.Fatalf("Dropped() = %d, want %d (records past the subscriber's %d-slot buffer)",
			got, appendsPastChanCapacity, subscriberBufferSize)
	}
}

func TestRingBuffer_CountsNoDropWhileTheSubscriberKeepsUp(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	ch, cancel, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()

	for range subscriberBufferSize * 2 {
		logger.Info("drained")
		<-ch
	}

	if got := ring.Dropped(); got != 0 {
		t.Fatalf("Dropped() = %d, want 0 (every record was delivered)", got)
	}
}

func TestRingBuffer_ConcurrentAppends(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 150 {
				logger.Info("c")
			}
		}()
	}
	wg.Wait()
	if got := len(ring.Snapshot()); got != ringCapacity {
		t.Fatalf("snapshot len = %d, want %d (capacity)", got, ringCapacity)
	}
}

func TestRingBuffer_SubscribeRejectsPastCeiling(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	chans := make([]<-chan logging.CapturedRecord, 0, logging.MaxSubscribers)
	cancels := make([]func(), 0, logging.MaxSubscribers)
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()
	for i := 0; i < logging.MaxSubscribers; i++ {
		ch, cancel, err := ring.Subscribe()
		if err != nil {
			t.Fatalf("subscriber %d: %v", i+1, err)
		}
		chans = append(chans, ch)
		cancels = append(cancels, cancel)
	}

	if ch, cancel, err := ring.Subscribe(); !errors.Is(err, logging.ErrTooManySubscribers) || ch != nil || cancel != nil {
		t.Fatalf("subscribe past ceiling = (%v, %v, %v), want ErrTooManySubscribers and no channel", ch, cancel != nil, err)
	}

	logger.Info("still-live")
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

func TestNewRingBuffer_StartsEmptyAndAcceptsSubscribers(t *testing.T) {
	ring := logging.MustNewRingBuffer(0, time.Now)

	_, cancel, err := ring.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer cancel()
	if snap := ring.Snapshot(); len(snap) != 0 || ring.Dropped() != 0 {
		t.Fatalf("new ring holds %d records and %d drops, want none", len(snap), ring.Dropped())
	}
}

func TestRingHandler_ShortRecordStoredUnchangedAndStdoutGetsFullText(t *testing.T) {
	logger, ring, stdout := setupLogger(t, "debug", false)
	long := strings.Repeat("x", 1<<20)

	logger.Info("short message", "k", "short value")
	logger.Info(long)

	snap := ring.Snapshot()
	if snap[0].Message != "short message" || snap[0].Attrs["k"] != "short value" {
		t.Errorf("short record altered: %+v", snap[0])
	}
	if !strings.Contains(stdout(), `"msg":"`+long+`"`) {
		t.Errorf("stdout lost the full %d-byte message the ring truncates", len(long))
	}
}

func TestRingHandler_StoresTextAtTheCapUnmarked(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	atCap := strings.Repeat("y", ringTextCapBytes)

	logger.Info(atCap)

	if got := ring.Snapshot()[0].Message; got != atCap {
		t.Fatalf("message of exactly %d bytes stored as %d bytes, want it unchanged", ringTextCapBytes, len(got))
	}
}

func TestRingHandler_TruncatesOnARuneBoundary(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	offByOne := "a" + strings.Repeat("é", ringTextCapBytes)

	logger.Info(offByOne)

	got := ring.Snapshot()[0].Message
	kept, _, found := strings.Cut(got, "…[truncated ")
	if !found || len(kept) != ringTextCapBytes-1 || strings.ToValidUTF8(kept, "") != kept {
		t.Fatalf("kept %d bytes (valid UTF-8 %v), want %d bytes cut before the split rune", len(kept), strings.ToValidUTF8(kept, "") == kept, ringTextCapBytes-1)
	}
	if !strings.HasSuffix(got, fmt.Sprintf("[truncated %d]", len(offByOne)-len(kept))) {
		t.Fatalf("marker %q does not count the %d dropped bytes", got[len(kept):], len(offByOne)-len(kept))
	}
}

func TestRingHandler_BoundsStoredMessageAndAttrWithMarker(t *testing.T) {
	logger, ring := newCaptureLogger(t)
	huge := strings.Repeat("é", 512*1024)

	logger.Error(huge, "error", huge)

	rec := ring.Snapshot()[0]
	for name, text := range map[string]string{"msg": rec.Message, "error": rec.Attrs["error"]} {
		if len(text) > ringTextCapBytes+64 {
			t.Errorf("%s stored %d bytes, want bounded", name, len(text))
		}
		if !strings.Contains(text, "…[truncated ") || !strings.HasSuffix(text, "]") {
			t.Errorf("%s lacks truncation marker: %.40q", name, text[max(0, len(text)-40):])
		}
		if strings.ToValidUTF8(text, "") != text {
			t.Errorf("%s is not valid UTF-8", name)
		}
	}
}

func TestRingHandler_WithGroupPrefixesLaterAttrs(t *testing.T) {
	logger, ring := newCaptureLogger(t)

	logger.WithGroup("g").With("k", 1).Info("grouped", "r", 2)

	snap := ring.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("want 1 record, got %+v", snap)
	}
	attrs := snap[0].Attrs
	if attrs["g.k"] != "1" || attrs["g.r"] != "2" {
		t.Fatalf("group prefix lost: %+v", attrs)
	}
	if _, ok := attrs["k"]; ok {
		t.Fatalf("unprefixed key leaked: %+v", attrs)
	}
}

func TestRingHandler_SiblingGroupsWriteDistinctKeys(t *testing.T) {
	logger, ring := newCaptureLogger(t)

	logger.WithGroup("a").With("k", 1).Info("a")
	logger.WithGroup("b").With("k", 2).Info("b")
	logger.With("k", 3).Info("plain")

	for _, rec := range ring.Snapshot() {
		switch rec.Message {
		case "a":
			if rec.Attrs["a.k"] != "1" {
				t.Fatalf("a: %+v", rec.Attrs)
			}
		case "b":
			if rec.Attrs["b.k"] != "2" {
				t.Fatalf("b: %+v", rec.Attrs)
			}
		case "plain":
			if rec.Attrs["k"] != "3" {
				t.Fatalf("plain: %+v", rec.Attrs)
			}
		}
	}
}
