package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/expiringcache"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"altune/go-api/internal/shared/parallel"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	relatedTimeout     = 2 * time.Second
	RelatedTopN        = 5
	relatedPerGroup    = 10
	maxProviderLookups = 5

	relatedMemoTTL        = time.Minute
	relatedMemoMaxEntries = 4096
)

type FindRelatedService struct {
	sources   map[domain.ResultKind][]LookupSource
	perResult int
	memo      *expiringcache.Memo[relatedMemoKey, []domain.SearchResult]
}

func NewFindRelatedService(now clock.Func, sources ...LookupSource) (*FindRelatedService, error) {
	if now == nil {
		return nil, fmt.Errorf("%w: a clock is nil", domain.ErrRelatedCollaboratorRequired)
	}
	byKind := make(map[domain.ResultKind][]LookupSource, len(sources))
	perResult := 0
	for i, source := range sources {
		if source == nil {
			return nil, fmt.Errorf("%w: source %d is nil", domain.ErrRelatedCollaboratorRequired, i)
		}
		kind := source.resultKind()
		byKind[kind] = append(byKind[kind], source)
		perResult = max(perResult, len(byKind[kind]))
	}
	return &FindRelatedService{
		sources:   byKind,
		perResult: perResult,
		memo:      expiringcache.New[relatedMemoKey, []domain.SearchResult](relatedMemoTTL, relatedMemoMaxEntries, now),
	}, nil
}

func (s *FindRelatedService) Execute(
	ctx context.Context,
	userId ids.UserId,
	organicResults []domain.SearchResult,
) []domain.RelatedGroup {
	ctx, cancel := context.WithTimeout(ctx, relatedTimeout)
	defer cancel()

	topN := min(RelatedTopN, len(organicResults))
	if topN == 0 {
		return nil
	}

	fan := &relatedFanOut{lookups: parallel.New("related.lookup", s.perResult*topN), memo: s.memo}
	for _, result := range organicResults[:topN] {
		for _, source := range s.sources[result.Kind] {
			source.dispatch(ctx, fan, userId, result)
		}
	}
	groups := dedupRelatedAgainstOrganic(fan.wait(), organicResults)

	hits, misses := s.memo.Stats()
	slog.InfoContext(ctx, "related.complete", "groups", len(groups), "memoized", fan.memoHits, "memo_hits", hits, "memo_misses", misses)
	return groups
}

type relatedFanOut struct {
	lookups       *parallel.Limited
	memo          *expiringcache.Memo[relatedMemoKey, []domain.SearchResult]
	mu            sync.Mutex
	groups        []domain.RelatedGroup
	providerCalls atomic.Int32
	memoHits      int
}

func (f *relatedFanOut) memoize(lookup relatedGroupLookup, items []domain.SearchResult) []domain.SearchResult {
	f.memo.Put(lookup.memoKey(), items)
	return items
}

func (f *relatedFanOut) serveMemoized(lookup relatedGroupLookup) bool {
	items, memoized := f.memo.Get(lookup.memoKey())
	if !memoized {
		return false
	}
	f.memoHits++
	if len(items) > 0 {
		f.addGroup(lookup, items)
	}
	return true
}

type relatedPanicLog struct {
	event, key, value string
}

type relatedGroupLookup struct {
	relationship domain.RelationshipKind
	relatedTo    string
	owner        ids.UserId
	subject      string
	panicLog     relatedPanicLog
}

func (l relatedGroupLookup) memoKey() relatedMemoKey {
	return relatedMemoKey{relationship: l.relationship, owner: l.owner, subject: l.subject}
}

func libraryMatchesLookup(userId ids.UserId, result domain.SearchResult) relatedGroupLookup {
	return relatedGroupLookup{
		relationship: domain.RelationshipLibraryMatches,
		relatedTo:    result.Title,
		owner:        userId,
		subject:      result.Album + "\x00" + music.PlainNameKey(result.Subtitle),
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

func (f *relatedFanOut) fetchRelatedGroup(
	ctx context.Context,
	lookup relatedGroupLookup,
	fetch func(ctx context.Context) ([]domain.SearchResult, error),
) {
	f.lookups.Go(ctx, func() {
		defer background.RecoverContext(ctx, lookup.panicLog.event, lookup.panicLog.key, lookup.panicLog.value)
		items, err := fetch(ctx)
		if err != nil || len(items) == 0 {
			return
		}
		f.addGroup(lookup, items)
	})
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
	_ = f.lookups.Wait()
	return f.groups
}

type relatedMemoKey struct {
	relationship domain.RelationshipKind
	owner        ids.UserId
	subject      string
}

func truncateRelated(items []domain.SearchResult) []domain.SearchResult {
	if len(items) > relatedPerGroup {
		return items[:relatedPerGroup]
	}
	return items
}

func tryReserveProviderCall(calls *atomic.Int32, limit int) bool {
	for {
		cur := calls.Load()
		if cur >= int32(limit) {
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
			imageURL = domain.SafeImageURL(*m.ArtworkURL)
		}
		results = append(results, domain.SearchResult{
			Kind:       domain.ResultKindTrack,
			Title:      m.Title,
			Subtitle:   m.Artist,
			ImageURL:   imageURL,
			Confidence: domain.ConfidenceLow,
			Sources:    []domain.ProviderLink{},
			Album:      m.Album,
			Extras:     map[string]any{domain.ExtraAlbum: m.Album, "source": "library"},
		})
	}
	return results
}

func dedupRelatedAgainstOrganic(groups []domain.RelatedGroup, organic []domain.SearchResult) []domain.RelatedGroup {
	seen := make(map[string]bool, len(organic))
	for _, r := range organic {
		seen[domain.TitleSubtitleKey(r.Title, r.Subtitle)] = true
	}

	var filtered []domain.RelatedGroup
	for _, g := range groups {
		var items []domain.SearchResult
		for _, item := range g.Items {
			key := domain.TitleSubtitleKey(item.Title, item.Subtitle)
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
