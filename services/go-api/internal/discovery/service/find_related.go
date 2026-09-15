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
)

type FindRelatedService struct {
	querier        ports.RelationshipQuerier
	albumProvider  ports.AlbumContentProvider
	artistProvider ports.ArtistContentProvider
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

	slog.InfoContext(ctx, "related.complete", "groups", len(groups))
	return groups
}

// dispatchLibraryMatches looks up the user's own library, so unlike the
// Deezer lookups it does not draw from the provider-call budget.
func (s *FindRelatedService) dispatchLibraryMatches(fan *relatedFanOut, userId shared.UserId, result domain.SearchResult) {
	album := result.Album
	if album == "" || s.querier == nil {
		return
	}
	fan.fetchRelatedGroup(domain.RelationshipLibraryMatches, result.Title,
		relatedPanicLog{event: "related.library_lookup_panic", key: "album", value: album},
		func(ctx context.Context) ([]domain.SearchResult, error) {
			matches, err := s.querier.FindRelatedByAlbum(ctx, userId, album, relatedPerGroup)
			if err != nil {
				slog.DebugContext(ctx, "related.library_lookup_failed", "error", err)
				return nil, err
			}
			return matchesToSearchResults(matches), nil
		})
}

func (s *FindRelatedService) dispatchAlbumTracks(fan *relatedFanOut, result domain.SearchResult) {
	albumID := result.DeezerAlbumID
	if albumID == "" || s.albumProvider == nil || !fan.reserveProviderCall() {
		return
	}
	fan.fetchRelatedGroup(domain.RelationshipAlbumTracks, result.Title,
		relatedPanicLog{event: "related.album_tracks_panic", key: "album_id", value: albumID},
		func(ctx context.Context) ([]domain.SearchResult, error) {
			tracks, err := s.albumProvider.GetAlbumTracks(ctx, domain.CanonicalContentProvider, albumID)
			return truncateRelated(tracks), err
		})
}

func (s *FindRelatedService) dispatchArtistAlbums(fan *relatedFanOut, result domain.SearchResult) {
	if s.artistProvider == nil {
		return
	}
	artistID := extractDeezerID(result)
	if artistID == "" || !fan.reserveProviderCall() {
		return
	}
	fan.fetchRelatedGroup(domain.RelationshipArtistAlbums, result.Title,
		relatedPanicLog{event: "related.artist_albums_panic", key: "artist_id", value: artistID},
		func(ctx context.Context) ([]domain.SearchResult, error) {
			albums, err := s.artistProvider.GetArtistAlbums(ctx, domain.CanonicalContentProvider, artistID)
			return truncateRelated(albums), err
		})
}

// relatedFanOut runs related-group lookups concurrently under one context and
// collects the non-empty successful groups in completion order.
type relatedFanOut struct {
	ctx           context.Context
	wg            sync.WaitGroup
	mu            sync.Mutex
	groups        []domain.RelatedGroup
	providerCalls atomic.Int32
}

// relatedPanicLog names the event and identifying attribute logged when a
// lookup goroutine panics.
type relatedPanicLog struct {
	event, key, value string
}

func (f *relatedFanOut) reserveProviderCall() bool {
	return tryReserveProviderCall(&f.providerCalls, maxProviderLookups)
}

// fetchRelatedGroup runs fetch in its own goroutine and records its items as
// one group. A failed, empty or panicking fetch contributes no group and does
// not affect the other lookups.
func (f *relatedFanOut) fetchRelatedGroup(
	relationship domain.RelationshipKind,
	relatedTo string,
	panicLog relatedPanicLog,
	fetch func(ctx context.Context) ([]domain.SearchResult, error),
) {
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		defer RecoverGoroutine(f.ctx, panicLog.event, panicLog.key, panicLog.value)
		items, err := fetch(f.ctx)
		if err != nil || len(items) == 0 {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.groups = append(f.groups, domain.RelatedGroup{
			Relationship: relationship,
			RelatedTo:    relatedTo,
			Items:        items,
		})
	}()
}

func (f *relatedFanOut) wait() []domain.RelatedGroup {
	f.wg.Wait()
	return f.groups
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
