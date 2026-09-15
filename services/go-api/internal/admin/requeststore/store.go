package requeststore

import (
	"context"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/httputil"
)

const (
	defaultMaxRequests  = 100
	defaultMaxBodyBytes = 64 * 1024
	defaultMaxTotalByte = 96 * 1024 * 1024
	// retentionWindow bounds how long a trace stays readable. Records hold a
	// user's search query, user id and raw provider bodies, so they are purged
	// once older than this, independent of the byte/count eviction budgets.
	retentionWindow = 30 * time.Minute
)

type Store struct {
	mu          sync.Mutex
	order       []string
	byID        map[string]*RequestRecord
	totalBytes  int
	maxRequests int
	maxBody     int
	maxTotal    int
	retention   time.Duration
	// now stamps new trace records with a monotonic-bearing instant; since
	// measures a record's age from that instant. Both are monotonic-safe
	// (immune to wall-clock steps) in production and injectable so tests can
	// diverge wall time from elapsed time.
	now   func() time.Time
	since func(time.Time) time.Duration
}

func New() *Store {
	return newWithClock(time.Now, time.Since)
}

func newWithClock(now func() time.Time, since func(time.Time) time.Duration) *Store {
	return &Store{
		byID:        make(map[string]*RequestRecord),
		maxRequests: defaultMaxRequests,
		maxBody:     defaultMaxBodyBytes,
		maxTotal:    defaultMaxTotalByte,
		retention:   retentionWindow,
		now:         now,
		since:       since,
	}
}

func (s *Store) MaxBodyBytes() int { return s.maxBody }

// recordExchange appends ex to corrID's record. started is the exchange's
// monotonic-bearing start instant (ex.At is its UTC, wall-only rendering), so
// a record created here ages from a reading immune to wall-clock steps.
func (s *Store) recordExchange(corrID string, ex Exchange, started time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.getOrCreateLocked(corrID, started)
	rec.Exchanges = append(rec.Exchanges, ex)
	rec.bytes += len(ex.RespBody)
	s.totalBytes += len(ex.RespBody)
	s.evictForBytes()
}

// RecordSearch is a no-op when ctx carries no correlation id.
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
	rec := s.getOrCreateLocked(corrID, s.now())
	rec.Query = query
	rec.Kinds = kinds
	rec.User = user
	rec.Providers = ProjectStatuses(statuses)
	rec.Final = ProjectResults(final)
}

// RecordContentFetch is a no-op when ctx carries no correlation id.
func (s *Store) RecordContentFetch(
	ctx context.Context,
	ev ports.ContentFetchEvent,
	items []domain.SearchResult,
) {
	corrID := httputil.GetCorrelationID(ctx)
	if corrID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.getOrCreateLocked(corrID, s.now())
	rec.Detail = &DetailTrace{
		Kind:     ev.Kind,
		Provider: ev.Provider,
		Artist:   ev.Artist,
		Status:   ev.Status,
		Items:    projectDetailRows(items),
	}
}

// getOrCreateLocked keeps started verbatim as the retention stamp (it must
// retain its monotonic reading) and its UTC rendering for display; .UTC()
// strips the monotonic reading, so the two are held apart.
func (s *Store) getOrCreateLocked(corrID string, started time.Time) *RequestRecord {
	rec := s.byID[corrID]
	if rec != nil {
		return rec
	}
	rec = &RequestRecord{CorrID: corrID, StartedAt: started.UTC(), Exchanges: []Exchange{}, born: started}
	s.byID[corrID] = rec
	s.order = append(s.order, corrID)
	s.evictExpired()
	s.evictOverflow()
	return rec
}

// evictExpired purges every record older than the retention window. s.order
// is insertion order, not age order: an exchange is recorded when its body
// closes but ages from when its round trip started, so concurrent requests
// land out of age order. The whole order (bounded by maxRequests) is scanned
// and the survivors compacted in place.
func (s *Store) evictExpired() {
	kept := s.order[:0]
	for _, id := range s.order {
		if s.dropIfExpired(id) {
			continue
		}
		kept = append(kept, id)
	}
	clear(s.order[len(kept):])
	s.order = kept
}

// dropIfExpired removes corrID's record when it is past retention (or already
// gone) and reports whether it did. Age is since on the record's
// monotonic-bearing stamp, so a wall-clock step cannot make an expired record
// look fresh or a fresh one look expired.
func (s *Store) dropIfExpired(corrID string) bool {
	rec := s.byID[corrID]
	if rec != nil && s.since(rec.born) <= s.retention {
		return false
	}
	if rec != nil {
		s.totalBytes -= rec.bytes
		delete(s.byID, corrID)
	}
	return true
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
	if s.totalBytes > s.maxTotal && len(s.order) == 1 {
		s.trimOldestExchanges(s.byID[s.order[0]])
	}
}

func (s *Store) trimOldestExchanges(rec *RequestRecord) {
	for rec != nil && rec.bytes > s.maxTotal && len(rec.Exchanges) > 0 {
		dropped := rec.Exchanges[0]
		rec.Exchanges = rec.Exchanges[1:]
		rec.bytes -= len(dropped.RespBody)
		s.totalBytes -= len(dropped.RespBody)
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
	s.evictExpired()

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
	s.evictExpired()
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
