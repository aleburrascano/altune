package requeststore

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"strings"
	"testing"
)

// bigResults returns n results whose titles alone total n*titleLen bytes.
func bigResults(n, titleLen int) []domain.SearchResult {
	out := make([]domain.SearchResult, n)
	for i := range out {
		out[i] = domain.SearchResult{Kind: domain.ResultKindAlbum, Title: strings.Repeat("t", titleLen)}
	}
	return out
}

var traceRecorders = map[string]func(*Store, context.Context, []domain.SearchResult){
	"RecordSearch final": func(s *Store, ctx context.Context, items []domain.SearchResult) {
		s.RecordSearch(ctx, "q", nil, "u", nil, items)
	},
	"RecordSearch providers": func(s *Store, ctx context.Context, items []domain.SearchResult) {
		s.RecordSearch(ctx, "q", nil, "u", []domain.ProviderSearchResponse{{Results: items}}, nil)
	},
	"RecordContentFetch": func(s *Store, ctx context.Context, items []domain.SearchResult) {
		s.RecordContentFetch(ctx, ports.ContentFetchEvent{Kind: "albums"}, items)
	},
}

// TestTraceFields_CountTowardByteBudget pins that search/detail trace payloads
// are charged to the byte budget and evict older records, with no Exchange
// bodies involved at all.
func TestTraceFields_CountTowardByteBudget(t *testing.T) {
	for name, record := range traceRecorders {
		t.Run(name, func(t *testing.T) {
			s := New()
			record(s, logging.WithCorrelationID(t.Context(), "c1"), bigResults(50, 100))
			if s.totalBytes < 5_000 {
				t.Fatalf("totalBytes = %d after a 5000+ byte trace, want it charged", s.totalBytes)
			}
			// Room for one such trace but not two.
			s.maxTotal = s.totalBytes * 3 / 2
			record(s, logging.WithCorrelationID(t.Context(), "c2"), bigResults(50, 100))

			if _, ok := s.Get("c1"); ok {
				t.Error("c1 must be evicted once trace bytes exceed maxTotal")
			}
			if _, ok := s.Get("c2"); !ok {
				t.Error("newest record c2 must remain")
			}
			assertWithinBudget(t, s)
		})
	}
}

// TestTraceFields_ReRecordReplacesCharge pins that re-recording a trace on the
// same record replaces its charge rather than accumulating a phantom one.
func TestTraceFields_ReRecordReplacesCharge(t *testing.T) {
	for name, record := range traceRecorders {
		t.Run(name, func(t *testing.T) {
			s := New()
			ctx := logging.WithCorrelationID(t.Context(), "c1")
			record(s, ctx, bigResults(10, 100))
			once := s.totalBytes
			for range 20 {
				record(s, ctx, bigResults(10, 100))
			}
			if s.totalBytes != once {
				t.Errorf("totalBytes = %d after re-recording, want %d", s.totalBytes, once)
			}
			record(s, ctx, nil)
			if s.totalBytes >= once {
				t.Errorf("totalBytes = %d after shrinking the trace, want < %d", s.totalBytes, once)
			}
			assertWithinBudget(t, s)
		})
	}
}

// TestTraceFields_LoneOversizedRecordDoesNotPinMemory pins that a single
// record whose trace alone exceeds maxTotal is not left holding it.
func TestTraceFields_LoneOversizedRecordDoesNotPinMemory(t *testing.T) {
	for name, record := range traceRecorders {
		t.Run(name, func(t *testing.T) {
			s := New()
			s.maxTotal = 1_000
			s.recordExchange("c1", ex("body"), s.now())
			record(s, logging.WithCorrelationID(t.Context(), "c1"), bigResults(50, 100))
			assertWithinBudget(t, s)
		})
	}
}

// TestTraceFields_EvictedRecordReleasesTraceBytes pins that count eviction
// releases a record's trace charge along with the record.
func TestTraceFields_EvictedRecordReleasesTraceBytes(t *testing.T) {
	s := New()
	s.maxRequests = 1
	s.RecordSearch(logging.WithCorrelationID(t.Context(), "c1"), "q", nil, "u", nil, bigResults(10, 100))
	s.RecordContentFetch(logging.WithCorrelationID(t.Context(), "c2"), ports.ContentFetchEvent{Kind: "albums"}, nil)
	assertWithinBudget(t, s)
	if s.totalBytes >= 1_000 {
		t.Errorf("totalBytes = %d, want c1's 1000+ byte trace released", s.totalBytes)
	}
}

func assertWithinBudget(t *testing.T, s *Store) {
	t.Helper()
	sum := 0
	for _, id := range s.order {
		if rec := s.byID[id]; rec != nil {
			sum += rec.bytes
		}
	}
	if sum != s.totalBytes {
		t.Errorf("sum of record bytes = %d, totalBytes = %d; accounting drifted", sum, s.totalBytes)
	}
	if s.totalBytes > s.maxTotal {
		t.Errorf("totalBytes = %d exceeds maxTotal = %d", s.totalBytes, s.maxTotal)
	}
}
