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

type Record struct {
	CorrID    string     `json:"corr_id"`
	StartedAt time.Time  `json:"started_at"`
	Exchanges []Exchange `json:"exchanges"`

	Query     string          `json:"query,omitempty"`
	Kinds     []string        `json:"kinds,omitempty"`
	User      string          `json:"user,omitempty"`
	Providers []ProviderTrace `json:"providers,omitempty"`
	Final     []ResultRow     `json:"final,omitempty"`

	Detail *DetailTrace `json:"detail,omitempty"`

	bytes int
}

type DetailTrace struct {
	Kind     string      `json:"kind"`
	Provider string      `json:"provider"`
	Artist   string      `json:"artist,omitempty"`
	Status   string      `json:"status"`
	Items    []DetailRow `json:"items,omitempty"`
}

type DetailRow struct {
	Title            string `json:"title"`
	Year             int    `json:"year,omitempty"`
	ConsensusVerdict string `json:"status,omitempty"`
}

type ProviderTrace struct {
	Provider    string      `json:"provider"`
	Status      string      `json:"status"`
	LatencyMs   int64       `json:"latency_ms"`
	ResultCount int         `json:"result_count"`
	Err         string      `json:"error,omitempty"`
	Results     []ResultRow `json:"results,omitempty"`
}

type ResultRow struct {
	Kind                  string   `json:"kind"`
	Title                 string   `json:"title"`
	Subtitle              string   `json:"subtitle,omitempty"`
	ImageURL              string   `json:"image_url,omitempty"`
	Sources               []string `json:"sources,omitempty"`
	ArtworkSource         string   `json:"artwork_source,omitempty"`
	ArtworkResolutionPath string   `json:"artwork_path,omitempty"`
	ResolutionTier        string   `json:"resolution_tier,omitempty"`
	Confidence            string   `json:"confidence,omitempty"`
}

type Store struct {
	mu          sync.Mutex
	order       []string
	byID        map[string]*Record
	totalBytes  int
	maxRequests int
	maxBody     int
	maxTotal    int
}

func New() *Store {
	return &Store{
		byID:        make(map[string]*Record),
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
