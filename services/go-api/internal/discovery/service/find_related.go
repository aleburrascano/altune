package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	relatedTimeout     = 2 * time.Second
	relatedTopN        = 5
	relatedPerGroup    = 10
	maxProviderLookups = 5

	// relatedMemoTTL outlives the result cache's 45s TTL so that every request
	// served a warm slate is served warm related groups too; a related group may
	// be this far behind the user's library and the provider's catalogue.
	relatedMemoTTL = time.Minute
	// relatedMemoMaxEntries bounds the memo, whose keys grow with distinct
	// albums, artists and users rather than with the process's lifetime.
	relatedMemoMaxEntries = 4096
)

type FindRelatedService struct {
	querier        ports.RelationshipQuerier
	albumProvider  ports.AlbumContentProvider
	artistProvider ports.ArtistContentProvider
	memo           *relatedMemo
}

func NewFindRelatedService(
	querier ports.RelationshipQuerier,
	albumProvider ports.AlbumContentProvider,
	artistProvider ports.ArtistContentProvider,
) *FindRelatedService {
	return &FindRelatedService{
		querier:        querier,
		albumProvider:  albumProvider,
		artistProvider: artistProvider,
		memo:           newRelatedMemo(relatedMemoTTL),
	}
}

func (s *FindRelatedService) Execute(
	ctx context.Context,
	userId shared.UserId,
	organicResults []domain.SearchResult,
) []domain.RelatedGroup {
	ctx, cancel := context.WithTimeout(ctx, relatedTimeout)
	defer cancel()

	topN := min(relatedTopN, len(organicResults))
	if topN == 0 {
		return nil
	}

	fan := &relatedFanOut{ctx: ctx}
	for _, result := range organicResults[:topN] {
		switch result.Kind {
		case domain.ResultKindTrack:
			s.dispatchLibraryMatches(fan, userId, result)
			s.dispatchAlbumTracks(fan, result)
		case domain.ResultKindArtist:
			s.dispatchArtistAlbums(fan, result)
		}
	}
	groups := dedupRelatedAgainstOrganic(fan.wait(), organicResults)

	slog.InfoContext(ctx, "related.complete", "groups", len(groups), "memoized", fan.memoHits)
	return groups
}

// dispatchLibraryMatches looks up the user's own library, so unlike the
// Deezer lookups it does not draw from the provider-call budget.
func (s *FindRelatedService) dispatchLibraryMatches(fan *relatedFanOut, userId shared.UserId, result domain.SearchResult) {
	album := result.Album
	if album == "" || s.querier == nil {
		return
	}
	lookup := libraryMatchesLookup(userId, result)
	if s.serveMemoized(fan, lookup) {
		return
	}
	fan.fetchRelatedGroup(lookup, func(ctx context.Context) ([]domain.SearchResult, error) {
		matches, err := s.querier.FindRelatedByAlbum(ctx, userId, album, relatedPerGroup)
		if err != nil {
			slog.DebugContext(ctx, "related.library_lookup_failed", "error", err)
			return nil, err
		}
		return s.memo.remember(lookup.memoKey(), matchesToSearchResults(matches)), nil
	})
}

func (s *FindRelatedService) dispatchAlbumTracks(fan *relatedFanOut, result domain.SearchResult) {
	albumID := result.DeezerAlbumID
	if albumID == "" || s.albumProvider == nil {
		return
	}
	lookup := albumTracksLookup(albumID, result)
	if s.serveMemoized(fan, lookup) {
		return
	}
	if !fan.reserveProviderCall() {
		return
	}
	fan.fetchRelatedGroup(lookup, func(ctx context.Context) ([]domain.SearchResult, error) {
		tracks, err := s.albumProvider.GetAlbumTracks(ctx, domain.CanonicalContentProvider, albumID)
		if err != nil {
			return nil, err
		}
		return s.memo.remember(lookup.memoKey(), truncateRelated(tracks)), nil
	})
}

func (s *FindRelatedService) dispatchArtistAlbums(fan *relatedFanOut, result domain.SearchResult) {
	if s.artistProvider == nil {
		return
	}
	artistID := extractDeezerID(result)
	if artistID == "" {
		return
	}
	lookup := artistAlbumsLookup(artistID, result)
	if s.serveMemoized(fan, lookup) {
		return
	}
	if !fan.reserveProviderCall() {
		return
	}
	fan.fetchRelatedGroup(lookup, func(ctx context.Context) ([]domain.SearchResult, error) {
		albums, err := s.artistProvider.GetArtistAlbums(ctx, domain.CanonicalContentProvider, artistID)
		if err != nil {
			return nil, err
		}
		return s.memo.remember(lookup.memoKey(), truncateRelated(albums)), nil
	})
}

// serveMemoized answers a lookup from the memo when one is warm and reports
// whether it did. A memoized answer with no items is still an answer: it
// records that this subject has nothing related, and contributes no group.
func (s *FindRelatedService) serveMemoized(fan *relatedFanOut, lookup relatedGroupLookup) bool {
	items, memoized := s.memo.items(lookup.memoKey())
	if !memoized {
		return false
	}
	fan.memoHits++
	if len(items) > 0 {
		fan.addGroup(lookup, items)
	}
	return true
}

// relatedFanOut runs related-group lookups concurrently under one context and
// collects the non-empty successful groups in completion order. memoHits counts
// the lookups answered without a query or a call; only the dispatch goroutine
// touches it.
type relatedFanOut struct {
	ctx           context.Context
	wg            sync.WaitGroup
	mu            sync.Mutex
	groups        []domain.RelatedGroup
	providerCalls atomic.Int32
	memoHits      int
}

// relatedPanicLog names the event and identifying attribute logged when a
// lookup goroutine panics.
type relatedPanicLog struct {
	event, key, value string
}

// relatedGroupLookup is one related-group fetch: the group it produces, the
// subject it is about, and the owner whose rows it reads — the zero UserId when
// the answer is the same for every user. Deriving the memo key from all three
// is what keeps a user-scoped answer out of a shared entry.
type relatedGroupLookup struct {
	relationship domain.RelationshipKind
	relatedTo    string
	owner        shared.UserId
	subject      string
	panicLog     relatedPanicLog
}

func (l relatedGroupLookup) memoKey() relatedMemoKey {
	return relatedMemoKey{relationship: l.relationship, owner: l.owner, subject: l.subject}
}

func libraryMatchesLookup(userId shared.UserId, result domain.SearchResult) relatedGroupLookup {
	return relatedGroupLookup{
		relationship: domain.RelationshipLibraryMatches,
		relatedTo:    result.Title,
		owner:        userId,
		subject:      result.Album,
		panicLog:     relatedPanicLog{event: "related.library_lookup_panic", key: "album", value: result.Album},
	}
}

func albumTracksLookup(albumID string, result domain.SearchResult) relatedGroupLookup {
	return relatedGroupLookup{
		relationship: domain.RelationshipAlbumTracks,
		relatedTo:    result.Title,
		subject:      albumID,
		panicLog:     relatedPanicLog{event: "related.album_tracks_panic", key: "album_id", value: albumID},
	}
}

func artistAlbumsLookup(artistID string, result domain.SearchResult) relatedGroupLookup {
	return relatedGroupLookup{
		relationship: domain.RelationshipArtistAlbums,
		relatedTo:    result.Title,
		subject:      artistID,
		panicLog:     relatedPanicLog{event: "related.artist_albums_panic", key: "artist_id", value: artistID},
	}
}

func (f *relatedFanOut) reserveProviderCall() bool {
	return tryReserveProviderCall(&f.providerCalls, maxProviderLookups)
}

// fetchRelatedGroup runs fetch in its own goroutine and records its items as
// one group. A failed, empty or panicking fetch contributes no group and does
// not affect the other lookups.
func (f *relatedFanOut) fetchRelatedGroup(
	lookup relatedGroupLookup,
	fetch func(ctx context.Context) ([]domain.SearchResult, error),
) {
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer RecoverGoroutine(f.ctx, lookup.panicLog.event, lookup.panicLog.key, lookup.panicLog.value)
		items, err := fetch(f.ctx)
		if err != nil || len(items) == 0 {
			return
		}
		f.addGroup(lookup, items)
	}()
}

func (f *relatedFanOut) addGroup(lookup relatedGroupLookup, items []domain.SearchResult) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.groups = append(f.groups, domain.RelatedGroup{
		Relationship: lookup.relationship,
		RelatedTo:    lookup.relatedTo,
		Items:        items,
	})
}

func (f *relatedFanOut) wait() []domain.RelatedGroup {
	f.wg.Wait()
	return f.groups
}

// relatedMemoKey identifies one memoized lookup. owner is the zero UserId for
// the provider-backed lookups, whose answer is public catalogue data; a library
// lookup carries its user, so one user's rows can never key another's entry.
type relatedMemoKey struct {
	relationship domain.RelationshipKind
	owner        shared.UserId
	subject      string
}

type relatedMemoEntry struct {
	items     []domain.SearchResult
	expiresAt time.Time
}

// relatedMemo is the process-local memo of related lookups. Without it a slate
// served from the result cache still costs a library query and a provider call
// per top result, so the related fan-out's cost tracked request rate rather
// than the number of distinct queries. Concurrent identical searches still both
// fetch: collapsing them is singleflight's job, not this one's.
//
// A nil memo memoizes nothing, so a FindRelatedService assembled without one
// still answers.
type relatedMemo struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[relatedMemoKey]relatedMemoEntry
}

func newRelatedMemo(ttl time.Duration) *relatedMemo {
	return &relatedMemo{ttl: ttl, entries: map[relatedMemoKey]relatedMemoEntry{}}
}

func (m *relatedMemo) items(key relatedMemoKey) ([]domain.SearchResult, bool) {
	if m == nil {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, memoized := m.entries[key]
	if !memoized || time.Now().After(entry.expiresAt) {
		return nil, false
	}
	return entry.items, true
}

// remember stores items under key and hands them back, so a fetch can memoize
// and return in one line. Callers must treat a memoized slice as read-only.
func (m *relatedMemo) remember(key relatedMemoKey, items []domain.SearchResult) []domain.SearchResult {
	if m == nil {
		return items
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.entries) >= relatedMemoMaxEntries {
		m.dropExpired()
	}
	if len(m.entries) < relatedMemoMaxEntries {
		m.entries[key] = relatedMemoEntry{items: items, expiresAt: time.Now().Add(m.ttl)}
	}
	return items
}

// dropExpired sweeps every key, so it runs only when the memo is full: one pass
// over relatedMemoMaxEntries keys, and a memo that stays full stops taking new
// entries rather than growing. The caller holds m.mu.
func (m *relatedMemo) dropExpired() {
	now := time.Now()
	for key, entry := range m.entries {
		if now.After(entry.expiresAt) {
			delete(m.entries, key)
		}
	}
}

func truncateRelated(items []domain.SearchResult) []domain.SearchResult {
	if len(items) > relatedPerGroup {
		return items[:relatedPerGroup]
	}
	return items
}

func tryReserveProviderCall(calls *atomic.Int32, max int) bool {
	for {
		cur := calls.Load()
		if cur >= int32(max) {
			return false
		}
		if calls.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

func extractDeezerID(r domain.SearchResult) string {
	for _, src := range r.Sources {
		if domain.IsCanonicalContentProvider(src.Provider) {
			return src.ExternalID
		}
	}
	return ""
}

func matchesToSearchResults(matches []ports.RelatedTrackMatch) []domain.SearchResult {
	results := make([]domain.SearchResult, 0, len(matches))
	for _, m := range matches {
		imageURL := ""
		if m.ArtworkURL != nil {
			imageURL = *m.ArtworkURL
		}
		results = append(results, domain.SearchResult{
			Kind:       domain.ResultKindTrack,
			Title:      m.Title,
			Subtitle:   m.Artist,
			ImageURL:   imageURL,
			Confidence: domain.ConfidenceLow,
			Sources:    []domain.SourceRef{},
			Album:      m.Album,
			Extras:     map[string]any{"album": m.Album, "source": "library"},
		})
	}
	return results
}

func dedupRelatedAgainstOrganic(groups []domain.RelatedGroup, organic []domain.SearchResult) []domain.RelatedGroup {
	seen := make(map[string]bool, len(organic))
	for _, r := range organic {
		seen[textnorm.NormalizeForMatch(r.Title)+"|"+textnorm.NormalizeForMatch(r.Subtitle)] = true
	}

	var filtered []domain.RelatedGroup
	for _, g := range groups {
		var items []domain.SearchResult
		for _, item := range g.Items {
			key := textnorm.NormalizeForMatch(item.Title) + "|" + textnorm.NormalizeForMatch(item.Subtitle)
			if !seen[key] {
				seen[key] = true
				items = append(items, item)
			}
		}
		if len(items) > 0 {
			g.Items = items
			filtered = append(filtered, g)
		}
	}
	return filtered
}
