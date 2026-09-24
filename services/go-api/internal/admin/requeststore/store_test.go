package requeststore

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"testing"
	"time"
)

func ex(body string) Exchange {
	return Exchange{Method: "GET", URL: "https://api/x", Status: 200, RespBody: body, At: time.Now().UTC()}
}

func TestRecordExchange_CreatesAndAppends(t *testing.T) {
	s := New()
	s.recordExchange("c1", ex("a"), time.Now())
	s.recordExchange("c1", ex("b"), time.Now())

	rec, ok := s.Get("c1")
	if !ok {
		t.Fatal("record c1 not found")
	}
	if len(rec.Exchanges) != 2 {
		t.Fatalf("got %d exchanges, want 2", len(rec.Exchanges))
	}
}

func TestEviction_ByRequestCount(t *testing.T) {
	s := New()
	s.maxRequests = 3
	for _, id := range []string{"c1", "c2", "c3", "c4"} {
		s.recordExchange(id, ex("x"), time.Now())
	}
	if _, ok := s.Get("c1"); ok {
		t.Error("oldest record c1 should have been evicted")
	}
	if _, ok := s.Get("c4"); !ok {
		t.Error("newest record c4 should be retained")
	}
}

func TestEviction_ByTotalBytes(t *testing.T) {
	s := New()
	s.maxTotal = 10
	s.recordExchange("c1", ex("aaaaaa"), time.Now())
	s.recordExchange("c2", ex("bbbbbb"), time.Now())
	if _, ok := s.Get("c1"); ok {
		t.Error("c1 should be evicted to satisfy the byte ceiling")
	}
	if _, ok := s.Get("c2"); !ok {
		t.Error("c2 should remain")
	}
}

func TestEviction_SingleRecordCannotExceedBudget(t *testing.T) {
	s := New()
	s.maxTotal = 20
	// A reused correlation ID fans out to many provider exchanges, all
	// appended to the SAME record. s.order never grows, so count-based
	// eviction never fires and the record must not pin memory above cap.
	for range 50 {
		s.recordExchange("c1", ex("aaaaa"), time.Now()) // 5 bytes each -> 250 unbounded
	}
	if s.totalBytes > s.maxTotal {
		t.Fatalf("totalBytes=%d exceeds maxTotal=%d", s.totalBytes, s.maxTotal)
	}
	rec := s.byID["c1"]
	if rec == nil {
		t.Fatal("c1 should still be tracked")
	}
	if rec.bytes > s.maxTotal {
		t.Fatalf("record bytes=%d exceeds maxTotal=%d", rec.bytes, s.maxTotal)
	}
}

// TestRecordExchange_ExpiredOnArrivalIsNotCharged pins the byte leak: an
// exchange that closes more than a retention window after its round trip
// started must not open a record, because the eviction pass that runs on
// creation purges it and leaves its bytes with nothing to subtract them.
func TestRecordExchange_ExpiredOnArrivalIsNotCharged(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &steadyClock{at: base}
	s := newWithClock(clk.now, clk.since)

	s.recordExchange("c1", ex("late-body"), base.Add(-(retentionWindow + time.Minute)))

	if _, ok := s.Get("c1"); ok {
		t.Error("an exchange that arrived past retention must not be readable")
	}
	if s.totalBytes != 0 {
		t.Errorf("totalBytes = %d after an exchange that arrived expired, want 0", s.totalBytes)
	}
	assertWithinBudget(t, s)
}

// TestRecordExchange_OneRecordStaysBoundedUnderManyExchanges pins the missing
// per-record cap: empty bodies cost nothing against the byte budget, so nothing
// else stops one reused correlation id from accumulating exchanges forever.
func TestRecordExchange_OneRecordStaysBoundedUnderManyExchanges(t *testing.T) {
	s := New()
	empty := Exchange{Method: "GET", URL: "https://api/x", Status: 200, At: time.Now().UTC()}
	for range 100_000 {
		s.recordExchange("c1", empty, time.Now())
	}

	rec := s.byID["c1"]
	if rec == nil {
		t.Fatal("c1 should still be tracked")
	}
	if len(rec.Exchanges) > maxExchangesPerRecord {
		t.Errorf("record holds %d exchanges, want at most %d", len(rec.Exchanges), maxExchangesPerRecord)
	}
	assertWithinBudget(t, s)
}

// TestRecordExchange_ChargesEveryStringAnExchangeHolds pins that an exchange
// with an empty body still costs the budget: its URL, method and error text are
// memory the store holds just as much as the response body is.
func TestRecordExchange_ChargesEveryStringAnExchangeHolds(t *testing.T) {
	s := New()
	s.recordExchange("c1", Exchange{Method: "GET", URL: "https://api/very/long/path", Err: "dial timeout"}, time.Now())

	if s.totalBytes <= len("https://api/very/long/path")+len("dial timeout") {
		t.Errorf("totalBytes = %d, want the url, error and method charged plus overhead", s.totalBytes)
	}
	assertWithinBudget(t, s)
}

func resultsWithSource() []domain.SearchResult {
	return []domain.SearchResult{{
		Kind:    domain.ResultKindAlbum,
		Title:   "title",
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer}},
	}}
}

// TestRecordSearch_DoesNotAliasCallerKinds pins that the store copies the
// caller's slice: a caller reusing its buffer must not be able to rewrite an
// already-stored trace.
func TestRecordSearch_DoesNotAliasCallerKinds(t *testing.T) {
	s := New()
	kinds := []string{"album"}
	s.RecordSearch(logging.WithCorrelationID(t.Context(), "c1"), "q", kinds, "u", nil, nil)

	kinds[0] = "rewritten-by-caller"

	rec, ok := s.Get("c1")
	if !ok || len(rec.Kinds) != 1 || rec.Kinds[0] != "album" {
		t.Errorf("stored kinds = %v (found=%v), want [album]", rec.Kinds, ok)
	}
}

// TestSnapshot_DeepCopiesNestedSlices pins that a handed-out record shares no
// backing array with the live one. Kinds, a provider's Results and a row's
// Sources all survived the shallow copy, so mutating a snapshot reached back
// into the store.
func TestSnapshot_DeepCopiesNestedSlices(t *testing.T) {
	s := New()
	ctx := logging.WithCorrelationID(t.Context(), "c1")
	statuses := []domain.ProviderSearchResponse{{Results: resultsWithSource()}}
	s.RecordSearch(ctx, "q", []string{"album"}, "u", statuses, resultsWithSource())

	snap := s.Snapshot()[0]
	snap.Kinds[0] = "mutated"
	snap.Providers[0].Results[0].Sources[0] = "mutated"
	snap.Providers[0].Results[0].Title = "mutated"
	snap.Final[0].Sources[0] = "mutated"

	rec, _ := s.Get("c1")
	if rec.Kinds[0] != "album" {
		t.Errorf("stored kinds = %v, want [album]", rec.Kinds)
	}
	if rec.Providers[0].Results[0].Sources[0] != "deezer" || rec.Providers[0].Results[0].Title != "title" {
		t.Errorf("stored provider row = %+v, want it untouched", rec.Providers[0].Results[0])
	}
	if rec.Final[0].Sources[0] != "deezer" {
		t.Errorf("stored final sources = %v, want [deezer]", rec.Final[0].Sources)
	}
}

func TestSnapshot_NewestFirst_AndCopied(t *testing.T) {
	s := New()
	s.recordExchange("c1", ex("a"), time.Now())
	s.recordExchange("c2", ex("b"), time.Now())

	snap := s.Snapshot()
	if len(snap) != 2 || snap[0].CorrID != "c2" {
		t.Fatalf("want newest-first [c2,c1], got %v", snap)
	}
	snap[0].Exchanges[0].RespBody = "mutated"
	if rec, _ := s.Get("c2"); rec.Exchanges[0].RespBody != "b" {
		t.Error("snapshot must be a copy — mutating it changed the store")
	}
}

func TestGet_Miss(t *testing.T) {
	if _, ok := New().Get("nope"); ok {
		t.Error("Get on an unknown id should miss")
	}
}

// steadyClock is a fake whose since tracks its own reading, standing in for a
// monotonic clock that never steps.
type steadyClock struct{ at time.Time }

func (c *steadyClock) now() time.Time                  { return c.at }
func (c *steadyClock) since(t time.Time) time.Duration { return c.at.Sub(t) }

// steppedClock drives the now/since seams independently so a test can diverge
// wall time (now) from monotonic elapsed (since) the way an OS clock step does.
type steppedClock struct {
	wall    time.Time
	elapsed time.Duration
}

func (c *steppedClock) now() time.Time                { return c.wall }
func (c *steppedClock) since(time.Time) time.Duration { return c.elapsed }

func TestRetention_ExpiredRecordPurgedFromReadPath(t *testing.T) {
	clk := &steadyClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s := newWithClock(clk.now, clk.since)
	s.recordExchange("c1", Exchange{Method: "GET", URL: "u", Status: 200, RespBody: "secret-query", At: clk.at}, clk.at)

	// Age the record past the retention window without any byte/count pressure.
	clk.at = clk.at.Add(retentionWindow + time.Minute)

	if _, ok := s.Get("c1"); ok {
		t.Error("record older than the retention window must not be readable via Get")
	}
	if snap := s.Snapshot(); len(snap) != 0 {
		t.Errorf("expired record must not appear in Snapshot, got %d records", len(snap))
	}
}

func TestRetention_FreshRecordRetained(t *testing.T) {
	clk := &steadyClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	s := newWithClock(clk.now, clk.since)
	s.recordExchange("c1", Exchange{Method: "GET", URL: "u", Status: 200, RespBody: "a", At: clk.at}, clk.at)

	// Still comfortably inside the window.
	clk.at = clk.at.Add(retentionWindow - time.Minute)

	if _, ok := s.Get("c1"); !ok {
		t.Error("record within the retention window must remain readable")
	}
}

func TestRetention_TraceRecordsExpireAtBoundaryOnInjectedClock(t *testing.T) {
	record := map[string]func(*Store, context.Context){
		"RecordSearch": func(s *Store, ctx context.Context) {
			s.RecordSearch(ctx, "q", nil, "u", nil, nil)
		},
		"RecordContentFetch": func(s *Store, ctx context.Context) {
			s.RecordContentFetch(ctx, ports.ContentFetchEvent{Kind: "albums"}, nil)
		},
	}
	for name, recordFn := range record {
		t.Run(name, func(t *testing.T) {
			start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			clk := &steadyClock{at: start}
			s := newWithClock(clk.now, clk.since)
			recordFn(s, logging.WithCorrelationID(t.Context(), "c1"))

			rec, ok := s.Get("c1")
			if !ok || !rec.StartedAt.Equal(start) {
				t.Fatalf("StartedAt = %v (found=%v), want injected clock %v", rec.StartedAt, ok, start)
			}

			clk.at = start.Add(retentionWindow)
			if _, ok := s.Get("c1"); !ok {
				t.Error("record exactly at the retention boundary must remain readable")
			}

			clk.at = start.Add(retentionWindow + time.Nanosecond)
			if _, ok := s.Get("c1"); ok {
				t.Error("record just past the retention boundary must be purged")
			}
		})
	}
}

// TestRetention_PurgesExpiredRecordBehindYoungerHead pins gap 1: s.order is
// insertion order, and an exchange is inserted when its body closes but ages
// from when its round trip started. A slow request that started first but
// closed last sits behind a younger record, and must still be purged.
func TestRetention_PurgesExpiredRecordBehindYoungerHead(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &steadyClock{at: base}
	s := newWithClock(clk.now, clk.since)

	young := base
	old := base.Add(-20 * time.Minute)
	youngEx := Exchange{Method: "GET", URL: "u", Status: 200, RespBody: "a", At: young}
	s.recordExchange("young", youngEx, young)
	s.recordExchange("old", Exchange{Method: "GET", URL: "u", Status: 200, RespBody: "user-query", At: old}, old)

	// old is now 35m old (expired); young is 15m old (fresh).
	clk.at = base.Add(15 * time.Minute)

	if _, ok := s.Get("old"); ok {
		t.Error("expired record behind a younger head must be purged")
	}
	if _, ok := s.Get("young"); !ok {
		t.Error("fresh record must remain readable")
	}
	if snap := s.Snapshot(); len(snap) != 1 || snap[0].CorrID != "young" {
		t.Errorf("Snapshot = %v, want only [young]", snap)
	}
	if s.totalBytes != exchangeSize(youngEx) {
		t.Errorf("totalBytes = %d, want %d (purged record's bytes released)", s.totalBytes, exchangeSize(youngEx))
	}
}

// TestRetention_ImmuneToWallClockStep pins gap 2: retention must be judged by
// monotonic elapsed (since), not by comparing wall readings (now).
func TestRetention_ImmuneToWallClockStep(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ctx := logging.WithCorrelationID(t.Context(), "c1")

	t.Run("backward step still expires a stale record", func(t *testing.T) {
		clk := &steppedClock{wall: base}
		s := newWithClock(clk.now, clk.since)
		s.RecordSearch(ctx, "secret-query", nil, "u", nil, nil)

		// Wall clock steps back an hour, but 31 real minutes elapsed.
		clk.wall = base.Add(-time.Hour)
		clk.elapsed = retentionWindow + time.Minute

		if _, ok := s.Get("c1"); ok {
			t.Error("record past retention must be purged despite a backward wall-clock step")
		}
	})

	t.Run("forward jump keeps a fresh record", func(t *testing.T) {
		clk := &steppedClock{wall: base}
		s := newWithClock(clk.now, clk.since)
		s.RecordSearch(ctx, "q", nil, "u", nil, nil)

		// Wall clock jumps forward an hour; only a second of real time passed.
		clk.wall = base.Add(time.Hour)
		clk.elapsed = time.Second

		if _, ok := s.Get("c1"); !ok {
			t.Error("fresh record must survive a forward wall-clock jump")
		}
	})
}
