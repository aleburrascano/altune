package ports

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
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
	EraseSearchTextForUser(ctx context.Context, userId shared.UserId) error
}

var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

type DeletedIdentityEraser interface {
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
}

type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
}

type ActivityFeed interface {
	EmitActivity(eventType string)
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

type DiscographyCase struct {
	ArtistRef          string
	Releases           int
	SingleProvider     int
	SingleProviderNoID int
	ProviderCounts     map[string]int
	LastSeen           time.Time
}

type DiscographyGroupBy string

const (
	GroupByArtist            DiscographyGroupBy = "artist"
	GroupByProvider          DiscographyGroupBy = "provider"
	GroupByContaminationBand DiscographyGroupBy = "contamination_band"
)

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

type DiscographySuspectRate struct {
	Rate       float64
	LastSample time.Time
}

type DiscographyQualityReader interface {
	DiscographyQuality(ctx context.Context, since time.Time, groupBy DiscographyGroupBy, limit int) ([]DiscographyCase, error)
	SuspectRate(ctx context.Context, since time.Time) (DiscographySuspectRate, error)
}

type DiscographyPruner interface {
	PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error)
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
	Name() domain.ProviderName
	FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error)
}
