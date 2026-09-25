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

// HistoryEraser backs the clear-history request. An account's search text is
// kept in two places — the history rows it reads back, and the query_norm
// discovery_events carries for ranking — so an erasure that reaches only the
// first leaves the queries the user asked to forget tied to their user_id for
// the whole telemetry retention window (#2237).
type HistoryEraser interface {
	// EraseSearchTextForUser removes every search text stored against userId,
	// in one transaction: no reader sees the history gone while the telemetry
	// still names what was searched for. An account with nothing stored is not
	// an error.
	EraseSearchTextForUser(ctx context.Context, userId shared.UserId) error
}

// ErrIdentityStoreUnavailable reports that the identity store cannot be read
// from here — absent (a plain Postgres carrying no Supabase auth schema) or not
// granted to this role. Callers idle on it rather than erasing: "no identity is
// visible" must never be acted on as "every identity was deleted". Discovery
// declares its own rather than sharing playback's: the two modules never import
// each other (depguard discovery-boundary).
var ErrIdentityStoreUnavailable = errors.New("identity store unavailable")

// DeletedIdentityEraser erases one discovery table's rows for accounts that no
// longer exist. Supabase owns identities out-of-band, deletes one without
// telling this service, and discovery_search_history, discovery_favorites and
// discovery_events carry no foreign key to cascade from, so a deleted account's
// search text, favorites and telemetry outlive it unless something asks (#2236).
type DeletedIdentityEraser interface {
	// EraseRowsOfDeletedIdentities deletes every row whose owner is gone from
	// the identity store and reports how many it removed. It erases nothing and
	// returns an error satisfying errors.Is(err, ErrIdentityStoreUnavailable)
	// when the identity store cannot be read, so an unreadable store is never
	// taken as proof that every account was deleted.
	//
	// shared.SystemUserId is never erased: it is absent from the identity store
	// by design, not by deletion, and the rows it owns are server-emitted rather
	// than any account's.
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
}

type EventStore interface {
	Append(ctx context.Context, event domain.InteractionEvent) error
}

// AdminActivity surfaces one recorded interaction's type onto the operator
// event feed, carrying no user id and no payload: the admin console gets to
// see that a search happened or a track played, never who or what (#2585).
type AdminActivity interface {
	Emit(eventType string)
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
// server-emitted discography_observed events. SingleProvider is the count of
// releases exactly one provider supplied; SingleProviderNoID is how many of those
// also lack a shared id — the real contamination suspects the worst-first order
// ranks on, since a lone-provider release still carrying a strong/verified id is
// not a suspect. ProviderCounts is the per-provider release count. The verdict is
// computed in go-api at the merge — a reader never recomputes it.
type DiscographyCase struct {
	ArtistRef          string
	Releases           int
	SingleProvider     int
	SingleProviderNoID int
	ProviderCounts     map[string]int
	LastSeen           time.Time
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

// DiscographySuspectRate is the windowed headline: the share of real discography
// opens whose top release-suspect fired. Rate is suspectOpens/opens in [0,1] — an
// open counts as a suspect when its single_provider_no_id (the id-anchored top
// suspect from #1800) is > 0 — and is 0 when no open was recorded, never a
// divide-by-zero. LastSample is the occurred_at of the most recent open in the
// window (zero when none), so the reader can show the headline's freshness. Every
// discography_observed event is a real production open — eval/synthetic traffic
// emits none — so the rate is over real requests only, by construction.
type DiscographySuspectRate struct {
	Rate       float64
	LastSample time.Time
}

// DiscographyQualityReader serves the discography structural-quality cases over a
// bounded window, worst-first: the artist with the highest no-id suspect ratio
// (single-provider-without-a-shared-id releases over total) ranks first, with the
// plain single-provider headcount ratio as the fallback tie-break. groupBy
// re-clusters that same worst-first order by artist, provider, or contamination
// band. SuspectRate is the same window's headline — the share of real opens whose
// top suspect fired. The verdict is computed in go-api at the merge; this read
// only orders and counts it.
type DiscographyQualityReader interface {
	DiscographyQuality(ctx context.Context, since time.Time, groupBy DiscographyGroupBy, limit int) ([]DiscographyCase, error)
	SuspectRate(ctx context.Context, since time.Time) (DiscographySuspectRate, error)
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
	Name() domain.ProviderName
	FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error)
}
