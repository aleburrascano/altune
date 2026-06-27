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
	s.recordExchange("c1", ex("aaaaaa")) // 6 bytes
	s.recordExchange("c2", ex("bbbbbb")) // 6 bytes → total 12 > 10, drop c1
	if _, ok := s.Get("c1"); ok {
		t.Error("c1 should be evicted to satisfy the byte ceiling")
	}
	if _, ok := s.Get("c2"); !ok {
		t.Error("c2 should remain")
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
