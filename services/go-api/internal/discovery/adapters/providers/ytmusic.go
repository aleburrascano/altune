package providers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"altune/go-api/internal/discovery/domain"

	"github.com/raitonoberu/ytmusic"
)

type YouTubeMusicAdapter struct{}

func NewYouTubeMusicAdapter() *YouTubeMusicAdapter {
	return &YouTubeMusicAdapter{}
}

func (a *YouTubeMusicAdapter) Name() domain.ProviderName { return domain.ProviderYouTube }

func (a *YouTubeMusicAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *YouTubeMusicAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	client := ytmusic.Search(query)
	result, err := client.Next()
	if err != nil {
		return nil, fmt.Errorf("ytmusic search: %w", err)
	}
	if result == nil {
		return []domain.SearchResult{}, nil
	}

	var results []domain.SearchResult

	if kinds[domain.ResultKindTrack] {
		for _, t := range result.Tracks {
			results = append(results, mapYTMusicTrack(t))
		}
	}
	if kinds[domain.ResultKindAlbum] {
		for _, a := range result.Albums {
			results = append(results, mapYTMusicAlbum(a))
		}
	}
	if kinds[domain.ResultKindArtist] {
		for _, a := range result.Artists {
			results = append(results, mapYTMusicArtist(a))
		}
	}

	return results, nil
}

func (a *YouTubeMusicAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	client := ytmusic.AlbumSearch(artistName)
	result, err := client.Next()
	if err != nil {
		return nil, fmt.Errorf("ytmusic album search: %w", err)
	}
	if result == nil {
		return []domain.SearchResult{}, nil
	}

	var results []domain.SearchResult
	for _, a := range result.Albums {
		artistMatch := false
		for _, artist := range a.Artists {
			if strings.EqualFold(artist.Name, artistName) {
				artistMatch = true
				break
			}
		}
		if !artistMatch {
			continue
		}
		results = append(results, mapYTMusicAlbum(a))
	}

	if len(result.Albums) > 0 && len(results) == 0 {
		slog.DebugContext(ctx, "ytmusic.no_artist_match",
			"artist", artistName,
			"albums_found", len(result.Albums),
		)
	}

	return results, nil
}

func (a *YouTubeMusicAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	client := ytmusic.TrackSearch(artistName)
	result, err := client.Next()
	if err != nil {
		return nil, fmt.Errorf("ytmusic track search: %w", err)
	}
	if result == nil {
		return []domain.SearchResult{}, nil
	}

	var results []domain.SearchResult
	for _, t := range result.Tracks {
		artistMatch := false
		for _, artist := range t.Artists {
			if strings.EqualFold(artist.Name, artistName) {
				artistMatch = true
				break
			}
		}
		if !artistMatch {
			continue
		}
		results = append(results, mapYTMusicTrack(t))
		if len(results) >= 10 {
			break
		}
	}

	return results, nil
}

func mapYTMusicTrack(t *ytmusic.TrackItem) domain.SearchResult {
	var subtitle string
	if len(t.Artists) > 0 {
		subtitle = t.Artists[0].Name
	}
	var imageURL string
	if len(t.Thumbnails) > 0 {
		imageURL = t.Thumbnails[len(t.Thumbnails)-1].URL
	}
	extras := make(map[string]any)
	if t.Duration > 0 {
		extras["duration"] = t.Duration
	}
	if t.Album.Name != "" {
		extras["album"] = t.Album.Name
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindTrack,
		Title:      t.Title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderYouTube,
			ExternalID: t.VideoID,
			URL:        "https://music.youtube.com/watch?v=" + t.VideoID,
		}},
		Extras: extras,
	}
}

func mapYTMusicAlbum(a *ytmusic.AlbumItem) domain.SearchResult {
	var subtitle string
	if len(a.Artists) > 0 {
		subtitle = a.Artists[0].Name
	}
	var imageURL string
	if len(a.Thumbnails) > 0 {
		imageURL = a.Thumbnails[len(a.Thumbnails)-1].URL
	}
	extras := make(map[string]any)
	if a.Year != "" {
		extras["year"] = a.Year
	}
	if a.Type != "" {
		extras["record_type"] = a.Type
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindAlbum,
		Title:      a.Title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderYouTube,
			ExternalID: a.BrowseID,
			URL:        "https://music.youtube.com/browse/" + a.BrowseID,
		}},
		Extras: extras,
	}
}

func mapYTMusicArtist(a *ytmusic.ArtistItem) domain.SearchResult {
	var imageURL string
	if len(a.Thumbnails) > 0 {
		imageURL = a.Thumbnails[len(a.Thumbnails)-1].URL
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      a.Artist,
		ImageURL:   imageURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderYouTube,
			ExternalID: a.BrowseID,
			URL:        "https://music.youtube.com/channel/" + a.BrowseID,
		}},
		Extras: make(map[string]any),
	}
}
