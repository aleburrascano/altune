package eventwatch_test

import (
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/events/eventstest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

const tapBufferSize = 256

func TestSubscribeAll_CapturesNeverSeenUser(t *testing.T) {
	tp := monitoring.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	freshUser := ids.NewUserId(uuid.New())
	tp.Publish(context.Background(), freshUser, eventstest.Fielded("track_added", map[string]any{"track_id": "secret"}))

	select {
	case evt := <-tap:
		if evt.Type != "track_added" {
			t.Errorf("type = %q, want track_added", evt.Type)
		}
		if evt.Timestamp.IsZero() {
			t.Error("tap event missing timestamp")
		}
	default:
		t.Fatal("event for a never-seen user did not reach the tap")
	}
}

func TestPublish_StampsCorrelationIDFromContext(t *testing.T) {
	tp := monitoring.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	ctx := logging.ContextWithCorrelationID(context.Background(), "req-abc123")
	tp.Publish(ctx, ids.NewUserId(uuid.New()), eventstest.Named("track_added"))

	select {
	case evt := <-tap:
		if evt.CorrID != "req-abc123" {
			t.Errorf("corr_id = %q, want req-abc123", evt.CorrID)
		}
	default:
		t.Fatal("event did not reach the tap")
	}
}

func TestPublish_ContextWithoutCorrelationIDYieldsEmptyCorrID(t *testing.T) {
	tp := monitoring.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	tp.Publish(context.Background(), ids.NewUserId(uuid.New()), eventstest.Named("track_added"))

	select {
	case evt := <-tap:
		if evt.CorrID != "" {
			t.Errorf("corr_id = %q, want empty for a context carrying none", evt.CorrID)
		}
	default:
		t.Fatal("event did not reach the tap")
	}
}

func TestSubscribeAll_SingleConsumer(t *testing.T) {
	tp := monitoring.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
	_, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("first SubscribeAll: %v", err)
	}
	defer cancel()

	if _, _, err := tp.SubscribeAll(); err == nil {
		t.Fatal("second SubscribeAll should error (single consumer)")
	}
}

func TestSubscribeAll_SlowConsumerDropsNotBlocks(t *testing.T) {
	tp := monitoring.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
	_, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	user := ids.NewUserId(uuid.New())
	for i := 0; i < 3*tapBufferSize; i++ {
		tp.Publish(context.Background(), user, eventstest.Named("spam"))
	}
	if tp.Dropped() == 0 {
		t.Error("expected some tap drops once the consumer buffer filled")
	}
}

func TestTap_ReleasedAfterCancel(t *testing.T) {
	tp := monitoring.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
	_, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	cancel()
	_, cancel2, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("re-subscribe after cancel: %v", err)
	}
	cancel2()
}

func TestPublish_SubjectOmitsUserAuthoredText(t *testing.T) {
	tp := monitoring.MustNewEventTap(events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor")), time.Now)
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	tp.Publish(context.Background(), ids.NewUserId(uuid.New()), eventstest.Fielded("playlist_created", map[string]any{"name": "my private playlist", "title": "a title"}))

	evt := <-tap
	if evt.Subject != "" {
		t.Errorf("subject = %q, want empty", evt.Subject)
	}
}

func TestTap_EmitReachesSubscriberWithoutTouchingInnerPublisher(t *testing.T) {
	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	tp := monitoring.MustNewEventTap(bus, time.Now)
	ch, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	before := bus.HighestIssuedID()
	tp.EmitActivity("search_performed")

	select {
	case evt := <-ch:
		if evt.Type != "search_performed" {
			t.Errorf("type = %q, want search_performed", evt.Type)
		}
		if evt.User != "" {
			t.Errorf("user = %q, want empty", evt.User)
		}
		if evt.Subject != "" {
			t.Errorf("subject = %q, want empty", evt.Subject)
		}
	default:
		t.Fatal("Emit did not reach the tap subscriber")
	}

	if after := bus.HighestIssuedID(); after != before {
		t.Errorf("HighestIssuedID moved from %d to %d, Emit must never publish to the inner bus", before, after)
	}
}

func newTap(t *testing.T, now func() time.Time) *monitoring.EventTap {
	t.Helper()
	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	return monitoring.MustNewEventTap(bus, now)
}

func TestTapClock_PublishedEventCarriesTheInjectedTimeInUTC(t *testing.T) {
	zone := time.FixedZone("plus2", 2*60*60)
	at := time.Date(2026, 5, 6, 12, 0, 0, 0, zone)
	tp := newTap(t, clocktest.NewManual(at).Func())
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	tp.Publish(context.Background(), ids.NewUserId(uuid.New()), eventstest.Named("track_added"))

	evt := <-tap
	if !evt.Timestamp.Equal(at) || evt.Timestamp.Location() != time.UTC {
		t.Errorf("timestamp = %v, want %v in UTC", evt.Timestamp, at.UTC())
	}
}

func TestTapClock_ActivityEventCarriesTheInjectedTime(t *testing.T) {
	at := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	tp := newTap(t, clocktest.NewManual(at).Func())
	tap, cancel, err := tp.SubscribeAll()
	if err != nil {
		t.Fatalf("SubscribeAll: %v", err)
	}
	defer cancel()

	tp.EmitActivity("log_burst")

	if evt := <-tap; !evt.Timestamp.Equal(at) {
		t.Errorf("timestamp = %v, want %v", evt.Timestamp, at)
	}
}

func TestNewEventTap_RefusesANilClock(t *testing.T) {
	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))

	tp, err := monitoring.NewEventTap(bus, nil)

	if !errors.Is(err, monitoring.ErrTapClockRequired) || tp != nil {
		t.Fatalf("NewEventTap(nil clock) = (%v, %v), want ErrTapClockRequired and no tap", tp, err)
	}
}
