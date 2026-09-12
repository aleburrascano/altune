package requeststore

import (
	"testing"
	"time"
)

func ex(body string) Exchange {
	return Exchange{Method: "GET", URL: "https://api/x", Status: 200, RespBody: body, At: time.Now().UTC()}
}

func TestRecordExchange_CreatesAndAppends(t *testing.T) {
	s := New()
	s.recordExchange("c1", ex("a"))
	s.recordExchange("c1", ex("b"))

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
		s.recordExchange(id, ex("x"))
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
	s.recordExchange("c1", ex("aaaaaa"))
	s.recordExchange("c2", ex("bbbbbb"))
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
		s.recordExchange("c1", ex("aaaaa")) // 5 bytes each -> 250 unbounded
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

func TestSnapshot_NewestFirst_AndCopied(t *testing.T) {
	s := New()
	s.recordExchange("c1", ex("a"))
	s.recordExchange("c2", ex("b"))

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

func TestRetention_ExpiredRecordPurgedFromReadPath(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newWithClock(func() time.Time { return clock })
	s.recordExchange("c1", Exchange{Method: "GET", URL: "u", Status: 200, RespBody: "secret-query", At: clock})

	// Age the record past the retention window without any byte/count pressure.
	clock = clock.Add(retentionWindow + time.Minute)

	if _, ok := s.Get("c1"); ok {
		t.Error("record older than the retention window must not be readable via Get")
	}
	if snap := s.Snapshot(); len(snap) != 0 {
		t.Errorf("expired record must not appear in Snapshot, got %d records", len(snap))
	}
}

func TestRetention_FreshRecordRetained(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := newWithClock(func() time.Time { return clock })
	s.recordExchange("c1", Exchange{Method: "GET", URL: "u", Status: 200, RespBody: "a", At: clock})

	// Still comfortably inside the window.
	clock = clock.Add(retentionWindow - time.Minute)

	if _, ok := s.Get("c1"); !ok {
		t.Error("record within the retention window must remain readable")
	}
}
