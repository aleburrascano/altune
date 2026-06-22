package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
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
	organicResults []domain.SearchResult,
) []domain.RelatedGroup {
	ctx, cancel := context.WithTimeout(ctx, relatedTimeout)
	defer cancel()

	topN := relatedTopN
	if len(organicResults) < topN {
		topN = len(organicResults)
	}
	if topN == 0 {
		return nil
	}

	var (
		mu            sync.Mutex
		groups        []domain.RelatedGroup
		wg            sync.WaitGroup
		providerCalls int
	)

	for _, result := range organicResults[:topN] {
		if result.Kind == domain.ResultKindTrack {
			album := getStringExtra(result, "album")
			if album != "" && s.querier != nil {
				wg.Add(1)
				go func(r domain.SearchResult, albumName string) {
					defer wg.Done()
					matches, err := s.querier.FindRelatedByAlbum(ctx, albumName, relatedPerGroup)
					if err != nil {
						slog.DebugContext(ctx, "related.library_lookup_failed", "error", err)
						return
					}
					if len(matches) == 0 {
						return
					}
					items := matchesToSearchResults(matches)
					mu.Lock()
					groups = append(groups, domain.RelatedGroup{
						Relationship: "library_matches",
						RelatedTo:    r.Title,
						Items:        items,
					})
					mu.Unlock()
				}(result, album)
			}

			deezerAlbumID := getStringExtra(result, "deezer_album_id")
			if deezerAlbumID != "" && s.albumProvider != nil {
				mu.Lock()
				canCall := providerCalls < maxProviderLookups
				if canCall {
					providerCalls++
				}
				mu.Unlock()

				if canCall {
					wg.Add(1)
					go func(r domain.SearchResult, albumID string) {
						defer wg.Done()
						tracks, err := s.albumProvider.GetAlbumTracks(ctx, domain.ProviderDeezer, albumID)
						if err != nil || len(tracks) == 0 {
							return
						}
						if len(tracks) > relatedPerGroup {
							tracks = tracks[:relatedPerGroup]
						}
						mu.Lock()
						groups = append(groups, domain.RelatedGroup{
							Relationship: "album_tracks",
							RelatedTo:    r.Title,
							Items:        tracks,
						})
						mu.Unlock()
					}(result, deezerAlbumID)
				}
			}
		}

		if result.Kind == domain.ResultKindArtist && s.artistProvider != nil {
			deezerArtistID := extractDeezerID(result)
			if deezerArtistID == "" {
				continue
			}

			mu.Lock()
			canCall := providerCalls < maxProviderLookups
			if canCall {
				providerCalls++
			}
			mu.Unlock()

			if canCall {
				wg.Add(1)
				go func(r domain.SearchResult, artistID string) {
					defer wg.Done()
					albums, err := s.artistProvider.GetArtistAlbums(ctx, domain.ProviderDeezer, artistID)
					if err != nil || len(albums) == 0 {
						return
					}
					if len(albums) > relatedPerGroup {
						albums = albums[:relatedPerGroup]
					}
					mu.Lock()
					groups = append(groups, domain.RelatedGroup{
						Relationship: "artist_albums",
						RelatedTo:    r.Title,
						Items:        albums,
					})
					mu.Unlock()
				}(result, deezerArtistID)
			}
		}
	}

	wg.Wait()
	groups = dedupRelatedAgainstOrganic(groups, organicResults)

	slog.InfoContext(ctx, "related.complete", "groups", len(groups))
	return groups
}

func extractDeezerID(r domain.SearchResult) string {
	for _, src := range r.Sources {
		if src.Provider == domain.ProviderDeezer {
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
			Extras:     map[string]any{"album": m.Album, "source": "library"},
		})
	}
	return results
}

func dedupRelatedAgainstOrganic(groups []domain.RelatedGroup, organic []domain.SearchResult) []domain.RelatedGroup {
	organicKeys := make(map[string]bool, len(organic))
	for _, r := range organic {
		key := textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
		organicKeys[key] = true
	}

	var filtered []domain.RelatedGroup
	for _, g := range groups {
		var items []domain.SearchResult
		for _, item := range g.Items {
			key := textnorm.NormalizeForMatch(item.Title) + "|" + textnorm.NormalizeForMatch(item.Subtitle)
			if !organicKeys[key] {
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
