package ports

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
	"time"
)

type HistoryWriter interface {
	Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error
	TrimToN(ctx context.Context, userId shared.UserId, n int) error
}

type HistoryReader interface {
	ListDistinctRecent(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

type HistoryEraser interface {
	DeleteAllForUser(ctx context.Context, userId shared.UserId) error
}

type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
}

// ContentFetchEvent names the header of one artist-content fetch recorded on
// the operator request trace, so same-typed fields cannot be transposed.
type ContentFetchEvent struct {
	Kind     string
	Provider string
	Artist   string
	Status   string
}

type QueryCount struct {
	QueryNorm string
	Count     int
}

type BehavioralSignal struct {
	ResultSignature string
	Score           float64
}

type BehavioralSignalStore interface {
	SatisfactionSignals(ctx context.Context, since time.Time) ([]BehavioralSignal, error)
}

type BehavioralLabel struct {
	QueryNorm       string
	ResultSignature string
	Title           string
	Subtitle        string
	Polarity        int
}

type BehavioralLabelStore interface {
	BehavioralLabels(ctx context.Context, since time.Time) ([]BehavioralLabel, error)
}

type EventQuery interface {
	ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
	AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]QueryCount, error)
}

// DiscographyCase is one artist's structural-quality verdict, read back from the
// server-emitted discography_observed events. SingleProvider is the
// contamination-suspect count (releases exactly one provider supplied);
// ProviderCounts is the per-provider release count. The verdict is computed in
// go-api at the merge — a reader never recomputes it.
type DiscographyCase struct {
	ArtistRef      string
	Releases       int
	SingleProvider int
	ProviderCounts map[string]int
	LastSeen       time.Time
}

// DiscographyGroupBy is the dimension the worst-first case list is grouped on.
// It is a closed set: a request's raw by= value is parsed into one of these
// constants before it can reach a query, so an unrecognized, hostile, or
// injection-shaped value can never select a grouping or reach SQL — it falls
// back to the artist default.
type DiscographyGroupBy string

const (
	// GroupByArtist ranks each artist worst-first by its own contamination ratio.
	GroupByArtist DiscographyGroupBy = "artist"
	// GroupByProvider clusters artists by their dominant provider, worst cluster
	// first, so a provider whose artists are the most contamination-suspect leads.
	GroupByProvider DiscographyGroupBy = "provider"
	// GroupByContaminationBand buckets artists into high/medium/low contamination
	// bands, worst band first.
	GroupByContaminationBand DiscographyGroupBy = "contamination_band"
)

// ParseDiscographyGroupBy maps a raw by= param to a known grouping, defaulting to
// artist on an absent or unrecognized value. Defaulting (never echoing the raw
// string into a query or the response) is what keeps a hostile by= from selecting
// an unintended grouping or reaching the SQL.
func ParseDiscographyGroupBy(raw string) DiscographyGroupBy {
	switch DiscographyGroupBy(raw) {
	case GroupByProvider:
		return GroupByProvider
	case GroupByContaminationBand:
		return GroupByContaminationBand
	default:
		return GroupByArtist
	}
}

// DiscographyQualityReader serves the discography structural-quality cases over a
// bounded window, worst-first: the artist with the highest contamination ratio
// (single-provider releases over total, tie-broken by provider imbalance) ranks
// first. groupBy re-clusters that same worst-first order by artist, provider, or
// contamination band. The verdict is computed in go-api at the merge; this read
// only orders it.
type DiscographyQualityReader interface {
	DiscographyQuality(ctx context.Context, since time.Time, groupBy DiscographyGroupBy, limit int) ([]DiscographyCase, error)
}

// DiscographyPruner evicts discography_observed events older than the retention
// window, keeping discovery_events and the aggregate scan bounded no matter how
// many times a discography is opened. now is passed in (not read from the clock
// inside) so the cutoff is deterministic under test and the window lives with the
// prune rather than the caller.
type DiscographyPruner interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
}

type MetricPoint struct {
	AsOf  time.Time
	Value float64
}

type MetricsRollupStore interface {
	RollupDay(ctx context.Context, day time.Time) error
	MetricsHistory(ctx context.Context, metric string, days int) ([]MetricPoint, error)
}

type VocabularyReader interface {
	SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error)
	FindClosest(ctx context.Context, query string, limit int) ([]domain.VocabularyEntry, error)
}

type VocabularyWriter interface {
	Add(ctx context.Context, entry domain.VocabularyEntry) error
	BulkAdd(ctx context.Context, entries []domain.VocabularyEntry) error
	Trim(ctx context.Context, maxEntries int) error
}

type VocabularyStore interface {
	VocabularyReader
	VocabularyWriter
}

type ChartProvider interface {
	FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error)
}
