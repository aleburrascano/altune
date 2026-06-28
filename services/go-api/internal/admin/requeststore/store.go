// Package requeststore holds the in-memory, correlation-keyed store behind the
// Mission Control discovery drill-down. Each tracked request accumulates the
// outbound provider exchanges it made (captured by a bounded recording transport)
// plus — added by the discovery service — its query, user, pipeline stages, and
// final results. Bounded on three axes (requests retained, per-body bytes, total
// bytes) so it is safe on the 4 GB box; resets on restart like every other
// Mission Control surface.
package requeststore

import (
	"sync"
	"time"
)

const (
	defaultMaxRequests  = 100
	defaultMaxBodyBytes = 64 * 1024
	defaultMaxTotalByte = 96 * 1024 * 1024
)

// Exchange is one captured outbound provider call: how it went plus the response
// body, capped at the store's per-body limit.
type Exchange struct {
	Provider  string    `json:"provider,omitempty"`
	Method    string    `json:"method"`
	URL       string    `json:"url"`
	Status    int       `json:"status"`
	LatencyMs int64     `json:"latency_ms"`
	RespBody  string    `json:"response_body"`
	Truncated bool      `json:"truncated,omitempty"`
	Err       string    `json:"error,omitempty"`
	At        time.Time `json:"at"`
}

// Record is the whole story of one discovery request, keyed by correlation id.
// The exchanges are filled by the recording transport; the remaining fields are
// filled by the discovery service via the optional recorder seam (added in S2).
type Record struct {
	CorrID    string     `json:"corr_id"`
	StartedAt time.Time  `json:"started_at"`
	Exchanges []Exchange `json:"exchanges"`

	// Search-trace fields, filled at the discovery handler boundary (RecordSearch)
	// for the same corr_id the transport captured exchanges under.
	Query     string          `json:"query,omitempty"`
	Kinds     []string        `json:"kinds,omitempty"`
	User      string          `json:"user,omitempty"`
	Providers []ProviderTrace `json:"providers,omitempty"`
	Final     []ResultRow     `json:"final,omitempty"`

	// Detail-fetch trace, filled by RecordContentFetch for non-search discovery
	// requests (artist discography / top-tracks / related). Each detail fetch is its
	// own correlation id, so this and the search fields don't co-occur on one record.
	Detail *DetailTrace `json:"detail,omitempty"`

	bytes int // retained body bytes for this record, for the total ceiling
}

// DetailTrace is the high-level story of a detail-screen fetch — what the operator
// sees when they trace "what came up when I pressed this artist." Raw provider
// exchanges are captured separately under the same correlation id.
type DetailTrace struct {
	Kind     string      `json:"kind"` // "albums" | "top_tracks" | "related"
	Provider string      `json:"provider"`
	Artist   string      `json:"artist,omitempty"`
	Status   string      `json:"status"`
	Items    []DetailRow `json:"items,omitempty"`
}

// DetailRow is one returned item of a detail fetch, carrying the fields an operator
// needs to spot ordering/metadata bugs (year present? chronological? confirmed?).
type DetailRow struct {
	Title  string `json:"title"`
	Year   int    `json:"year,omitempty"`
	Status string `json:"status,omitempty"` // consensus verdict, when present
}

// ProviderTrace is one provider's contribution to a search: its outcome plus the
// mapped results it returned.
type ProviderTrace struct {
	Provider    string      `json:"provider"`
	Status      string      `json:"status"`
	LatencyMs   int64       `json:"latency_ms"`
	ResultCount int         `json:"result_count"`
	Results     []ResultRow `json:"results,omitempty"`
}

// ResultRow is the display projection of one search result — enough to eyeball
// correctness (title, artwork, contributing providers) without retaining the full
// domain object.
type ResultRow struct {
	Kind          string   `json:"kind"`
	Title         string   `json:"title"`
	Subtitle      string   `json:"subtitle,omitempty"`
	ImageURL      string   `json:"image_url,omitempty"`
	Sources       []string `json:"sources,omitempty"`
	ArtworkSource string   `json:"artwork_source,omitempty"` // provider that supplied the image
	ArtworkPath   string   `json:"artwork_path,omitempty"`   // how it resolved (identity/durable-identity/name/…)
}

// Store is concurrency-safe. The recording transport writes exchanges; the
// operator drill-down reads snapshots.
type Store struct {
	mu          sync.Mutex
	order       []string // corr_ids oldest→newest (the ring)
	byID        map[string]*Record
	totalBytes  int
	maxRequests int
	maxBody     int
	maxTotal    int
}

// New builds a store with the default bounds.
func New() *Store {
	return &Store{
		byID:        make(map[string]*Record),
		maxRequests: defaultMaxRequests,
		maxBody:     defaultMaxBodyBytes,
		maxTotal:    defaultMaxTotalByte,
	}
}

// MaxBodyBytes is the per-response capture cap the transport reads up to.
func (s *Store) MaxBodyBytes() int { return s.maxBody }

// recordExchange appends one captured exchange to its request, creating the
// request record on first sight and enforcing the request-count + total-byte
// bounds. Never blocks the caller's request path beyond the short store lock.
func (s *Store) recordExchange(corrID string, ex Exchange) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rec := s.getOrCreateLocked(corrID, ex.At)
	rec.Exchanges = append(rec.Exchanges, ex)
	rec.bytes += len(ex.RespBody)
	s.totalBytes += len(ex.RespBody)
	s.evictForBytes()
}

// getOrCreateLocked returns the record for corrID, creating it (and enforcing the
// request-count bound) on first sight. Caller holds the lock.
func (s *Store) getOrCreateLocked(corrID string, started time.Time) *Record {
	rec := s.byID[corrID]
	if rec != nil {
		return rec
	}
	rec = &Record{CorrID: corrID, StartedAt: started, Exchanges: []Exchange{}}
	s.byID[corrID] = rec
	s.order = append(s.order, corrID)
	s.evictOverflow()
	return rec
}

// evictOverflow drops oldest records until the count is within bounds. Caller
// holds the lock.
func (s *Store) evictOverflow() {
	for len(s.order) > s.maxRequests {
		s.dropOldest()
	}
}

// evictForBytes drops oldest records until the retained-body total is within the
// ceiling. Caller holds the lock.
func (s *Store) evictForBytes() {
	for s.totalBytes > s.maxTotal && len(s.order) > 1 {
		s.dropOldest()
	}
}

// dropOldest removes the oldest record. Caller holds the lock.
func (s *Store) dropOldest() {
	oldest := s.order[0]
	s.order = s.order[1:]
	if rec := s.byID[oldest]; rec != nil {
		s.totalBytes -= rec.bytes
		delete(s.byID, oldest)
	}
}

// Snapshot returns the retained records newest-first (copies; safe to serialize).
func (s *Store) Snapshot() []Record {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Record, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		if rec := s.byID[s.order[i]]; rec != nil {
			out = append(out, cloneRecord(rec))
		}
	}
	return out
}

// Get returns a copy of one request record by correlation id.
func (s *Store) Get(corrID string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.byID[corrID]
	if !ok {
		return Record{}, false
	}
	return cloneRecord(rec), true
}

func cloneRecord(rec *Record) Record {
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
	return Record{
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
