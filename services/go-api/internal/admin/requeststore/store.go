package requeststore

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"slices"
	"sync"
	"time"
)

const (
	defaultMaxRequests  = 100
	defaultMaxBodyBytes = 64 * 1024
	defaultMaxTotalByte = 96 * 1024 * 1024
	// maxExchangesPerRecord bounds a single record's exchange list. Empty
	// response bodies cost the byte budget almost nothing, so without a count
	// cap one long-lived correlation id grows one record without limit.
	maxExchangesPerRecord = 200
	// retentionWindow bounds how long a trace stays readable. Records hold a
	// user's search query, user id and raw provider bodies, so they are purged
	// once older than this, independent of the byte/count eviction budgets.
	retentionWindow = 30 * time.Minute
)

// Store is the bounded in-memory trace store behind the admin request
// inspector. It is safe for concurrent use: every path takes mu, so the
// recording transports write from whichever goroutine serves a request while
// the operator's reads run from another.
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
	if rec == nil {
		return
	}
	s.appendExchangeLocked(rec, ex)
	s.evictForBytes()
}

// appendExchangeLocked drops the oldest exchange once rec is full, so a
// correlation id reused across a long session cannot grow one record past
// maxExchangesPerRecord however small its bodies are.
func (s *Store) appendExchangeLocked(rec *RequestRecord, ex Exchange) {
	rec.Exchanges = append(rec.Exchanges, ex)
	s.moveBytesLocked(rec, exchangeSize(ex))
	for len(rec.Exchanges) > maxExchangesPerRecord {
		s.moveBytesLocked(rec, -exchangeSize(rec.Exchanges[0]))
		rec.Exchanges = rec.Exchanges[1:]
	}
}

func (s *Store) moveBytesLocked(rec *RequestRecord, delta int) {
	rec.bytes += delta
	s.totalBytes += delta
}

// RecordSearch is a no-op when ctx carries no correlation id. It retains none
// of the caller's backing arrays — kinds is cloned, statuses and final are
// projected — so the caller may reuse or mutate all three afterwards.
func (s *Store) RecordSearch(
	ctx context.Context,
	query string,
	kinds []string,
	user string,
	statuses []domain.ProviderSearchResponse,
	final []domain.SearchResult,
) {
	corrID := logging.CorrelationIDFromContext(ctx)
	if corrID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.getOrCreateLocked(corrID, s.now())
	if rec == nil {
		return
	}
	rec.Query = query
	rec.Kinds = slices.Clone(kinds)
	rec.User = user
	rec.Providers = ProjectStatuses(statuses)
	rec.Final = ProjectResults(final)
	s.chargeLocked(rec, &rec.searchBytes, searchTraceSize(query, kinds, user, rec.Providers, rec.Final))
}

// RecordContentFetch is a no-op when ctx carries no correlation id.
func (s *Store) RecordContentFetch(
	ctx context.Context,
	ev ports.ContentFetchEvent,
	items []domain.SearchResult,
) {
	corrID := logging.CorrelationIDFromContext(ctx)
	if corrID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec := s.getOrCreateLocked(corrID, s.now())
	if rec == nil {
		return
	}
	rec.Detail = &DetailTrace{
		Kind:     ev.Kind,
		Provider: ev.Provider,
		Artist:   ev.Artist,
		Status:   ev.Status,
		Items:    projectDetailRows(items),
	}
	s.chargeLocked(rec, &rec.detailBytes, detailTraceSize(rec.Detail))
}

// chargeLocked replaces the size held in one of rec's trace slots with size,
// moves rec's and the store's byte totals by the difference, and enforces the
// byte budget, so trace payloads are bounded exactly like exchange bodies.
func (s *Store) chargeLocked(rec *RequestRecord, slot *int, size int) {
	delta := size - *slot
	*slot = size
	rec.bytes += delta
	s.totalBytes += delta
	s.evictForBytes()
}

// getOrCreateLocked returns nil when started is already past retention: the
// eviction pass on creation would purge such a record immediately, and bytes
// charged to a record no longer in byID can never be subtracted again.
func (s *Store) getOrCreateLocked(corrID string, started time.Time) *RequestRecord {
	if rec := s.byID[corrID]; rec != nil {
		return rec
	}
	if s.since(started) > s.retention {
		return nil
	}
	return s.createLocked(corrID, started)
}

// createLocked keeps started verbatim as the retention stamp (it must retain
// its monotonic reading) and its UTC rendering for display; .UTC() strips the
// monotonic reading, so the two are held apart.
func (s *Store) createLocked(corrID string, started time.Time) *RequestRecord {
	rec := &RequestRecord{CorrID: corrID, StartedAt: started.UTC(), Exchanges: []Exchange{}, born: started}
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
	// A lone record still over budget with no exchanges left to shed is over
	// on its trace alone; it is dropped rather than left pinning the memory.
	if s.totalBytes > s.maxTotal && len(s.order) == 1 {
		s.dropOldest()
	}
}

func (s *Store) trimOldestExchanges(rec *RequestRecord) {
	for rec != nil && rec.bytes > s.maxTotal && len(rec.Exchanges) > 0 {
		s.moveBytesLocked(rec, -exchangeSize(rec.Exchanges[0]))
		rec.Exchanges = rec.Exchanges[1:]
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

// Snapshot returns every live record newest first, the order the console lists
// traces in. It purges the records past retention first, under the same lock,
// so no caller can read a trace the retention window has already released.
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

// Get reports whether corrID has a live record, purging the records past
// retention first, under the same lock, so an expired trace is never served.
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

// cloneRecord hands out a record that shares no backing array with the live
// one, nested slices included: a caller that mutates what it was given must not
// be able to reach back into the store.
func cloneRecord(rec *RequestRecord) RequestRecord {
	return RequestRecord{
		CorrID:    rec.CorrID,
		StartedAt: rec.StartedAt,
		Exchanges: slices.Clone(rec.Exchanges),
		Query:     rec.Query,
		Kinds:     slices.Clone(rec.Kinds),
		User:      rec.User,
		Providers: cloneProviderTraces(rec.Providers),
		Final:     cloneResultRows(rec.Final),
		Detail:    cloneDetailTrace(rec.Detail),
	}
}

func cloneProviderTraces(providers []ProviderTrace) []ProviderTrace {
	out := slices.Clone(providers)
	for i := range out {
		out[i].Results = cloneResultRows(out[i].Results)
	}
	return out
}

func cloneResultRows(rows []ResultRow) []ResultRow {
	out := slices.Clone(rows)
	for i := range out {
		out[i].Sources = slices.Clone(out[i].Sources)
	}
	return out
}

func cloneDetailTrace(detail *DetailTrace) *DetailTrace {
	if detail == nil {
		return nil
	}
	clone := *detail
	clone.Items = slices.Clone(detail.Items)
	return &clone
}
