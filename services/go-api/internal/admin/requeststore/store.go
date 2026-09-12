package requeststore

import (
	"context"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/httputil"
)

const (
	defaultMaxRequests  = 100
	defaultMaxBodyBytes = 64 * 1024
	defaultMaxTotalByte = 96 * 1024 * 1024
)

type Store struct {
	mu          sync.Mutex
	order       []string
	byID        map[string]*RequestRecord
	totalBytes  int
	maxRequests int
	maxBody     int
	maxTotal    int
}

func New() *Store {
	return &Store{
		byID:        make(map[string]*RequestRecord),
		maxRequests: defaultMaxRequests,
		maxBody:     defaultMaxBodyBytes,
		maxTotal:    defaultMaxTotalByte,
	}
}

func (s *Store) MaxBodyBytes() int { return s.maxBody }

func (s *Store) recordExchange(corrID string, ex Exchange) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.getOrCreateLocked(corrID, ex.At)
	rec.Exchanges = append(rec.Exchanges, ex)
	rec.bytes += len(ex.RespBody)
	s.totalBytes += len(ex.RespBody)
	s.evictForBytes()
}

func (s *Store) RecordSearch(
	ctx context.Context,
	query string,
	kinds []string,
	user string,
	statuses []domain.ProviderSearchResponse,
	final []domain.SearchResult,
) {
	corrID := httputil.GetCorrelationID(ctx)
	if corrID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.getOrCreateLocked(corrID, time.Now().UTC())
	rec.Query = query
	rec.Kinds = kinds
	rec.User = user
	rec.Providers = ProjectStatuses(statuses)
	rec.Final = ProjectResults(final)
}

func (s *Store) RecordContentFetch(
	ctx context.Context,
	kind, provider, artist, status string,
	items []domain.SearchResult,
) {
	corrID := httputil.GetCorrelationID(ctx)
	if corrID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.getOrCreateLocked(corrID, time.Now().UTC())
	rec.Detail = &DetailTrace{
		Kind:     kind,
		Provider: provider,
		Artist:   artist,
		Status:   status,
		Items:    projectDetailRows(items),
	}
}

func (s *Store) getOrCreateLocked(corrID string, started time.Time) *RequestRecord {
	rec := s.byID[corrID]
	if rec != nil {
		return rec
	}
	rec = &RequestRecord{CorrID: corrID, StartedAt: started, Exchanges: []Exchange{}}
	s.byID[corrID] = rec
	s.order = append(s.order, corrID)
	s.evictOverflow()
	return rec
}

func (s *Store) evictOverflow() {
	for len(s.order) > s.maxRequests {
		s.dropOldest()
	}
}

func (s *Store) evictForBytes() {
	for s.totalBytes > s.maxTotal && len(s.order) > 1 {
		s.dropOldest()
	}
}

func (s *Store) dropOldest() {
	oldest := s.order[0]
	s.order = s.order[1:]
	if rec := s.byID[oldest]; rec != nil {
		s.totalBytes -= rec.bytes
		delete(s.byID, oldest)
	}
}

func (s *Store) Snapshot() []RequestRecord {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]RequestRecord, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		if rec := s.byID[s.order[i]]; rec != nil {
			out = append(out, cloneRecord(rec))
		}
	}
	return out
}

func (s *Store) Get(corrID string) (RequestRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[corrID]
	if !ok {
		return RequestRecord{}, false
	}
	return cloneRecord(rec), true
}

func cloneRecord(rec *RequestRecord) RequestRecord {
	exchanges := make([]Exchange, len(rec.Exchanges))
	copy(exchanges, rec.Exchanges)
	providers := make([]ProviderTrace, len(rec.Providers))
	copy(providers, rec.Providers)
	final := make([]ResultRow, len(rec.Final))
	copy(final, rec.Final)
	var detail *DetailTrace
	if rec.Detail != nil {
		d := *rec.Detail
		d.Items = append([]DetailRow(nil), rec.Detail.Items...)
		detail = &d
	}
	return RequestRecord{
		CorrID:    rec.CorrID,
		StartedAt: rec.StartedAt,
		Exchanges: exchanges,
		Query:     rec.Query,
		Kinds:     rec.Kinds,
		User:      rec.User,
		Providers: providers,
		Final:     final,
		Detail:    detail,
	}
}
