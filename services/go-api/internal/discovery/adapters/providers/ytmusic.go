package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"

	"github.com/raitonoberu/ytmusic"
)

// ytmusicClientOnce bounds the ytmusic package's global HTTP client with a
// timeout. The library otherwise uses a no-timeout client and ignores the
// caller's context, so a slow/hung YouTube Music request could block a search
// indefinitely.
var ytmusicClientOnce sync.Once

func initYTMusicClient() {
	ytmusicClientOnce.Do(func() {
		ytmusic.HTTPClient = &http.Client{Timeout: 8 * time.Second}
	})
}

type YouTubeMusicAdapter struct{}

func NewYouTubeMusicAdapter() *YouTubeMusicAdapter {
	initYTMusicClient()
	return &YouTubeMusicAdapter{}
}

func (a *YouTubeMusicAdapter) Name() domain.ProviderName { return domain.ProviderYouTube }

// SearchTimeout gives YouTube Music a larger budget than the default fan-out
// timeout so the adapter has room to retry the intermittent rate-limit (HTTP
// 403, whose HTML body surfaces as a JSON parse error) it returns under bursty
// load.
func (a *YouTubeMusicAdapter) SearchTimeout() time.Duration { return 3 * time.Second }

// fetchYTMusic runs a ytmusic search with one retry on a transient error —
// notably the intermittent HTTP 403 rate-limit — while respecting the caller's
// context, which the library itself ignores.
func fetchYTMusic(ctx context.Context, newClient func() *ytmusic.SearchClient) (*ytmusic.SearchResult, error) {
	const attempts = 2
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := nextWithContext(ctx, newClient())
		if err == nil {
			return result, nil
		}
		lastErr = err
		if i < attempts-1 {
			select {
			case <-time.After(250 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	return nil, lastErr
}

// nextWithContext runs the (context-unaware) ytmusic call on a goroutine and
// returns as soon as the caller's context is done, so a slow request can't
// outlive the fan-out's deadline. The goroutine completes on its own under the
// client timeout, so it does not leak.
func nextWithContext(ctx context.Context, client *ytmusic.SearchClient) (*ytmusic.SearchResult, error) {
	type out struct {
		result *ytmusic.SearchResult
		err    error
	}
	ch := make(chan out, 1)
	go func() {
		result, err := client.Next()
		ch <- out{result, err}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case o := <-ch:
		return o.result, o.err
	}
}

func (a *YouTubeMusicAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *YouTubeMusicAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	result, err := fetchYTMusic(ctx, func() *ytmusic.SearchClient { return ytmusic.Search(query) })
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
		// AIDEV-NOTE: Coverage fix (plan 003 U6, Pattern C). YouTube Music
		// classifies many obscure/underground recordings as videos
		// (MUSIC_VIDEO_TYPE_OMV/UGC), which the ytmusic library routes to
		// result.Videos — not result.Tracks. Dropping them left the exact track
		// absent from the candidate set, so the ranker substituted the artist's
		// hit. Mapping videos as tracks recovers the recording; the categorical
		// merge dedups any video that duplicates an official track.
		for _, v := range result.Videos {
			results = append(results, mapYTMusicVideo(v))
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
	result, err := fetchYTMusic(ctx, func() *ytmusic.SearchClient { return ytmusic.AlbumSearch(artistName) })
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
	result, err := fetchYTMusic(ctx, func() *ytmusic.SearchClient { return ytmusic.TrackSearch(artistName) })
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

// mapYTMusicVideo maps a YouTube Music video result to a track. Used by the
// Pattern-C coverage fix: obscure recordings YT Music classifies as videos are
// still the playable track the user wants.
func mapYTMusicVideo(v *ytmusic.VideoItem) domain.SearchResult {
	var subtitle string
	if len(v.Artists) > 0 {
		subtitle = v.Artists[0].Name
	}
	var imageURL string
	if len(v.Thumbnails) > 0 {
		imageURL = v.Thumbnails[len(v.Thumbnails)-1].URL
	}
	extras := make(map[string]any)
	if v.Duration > 0 {
		extras["duration"] = v.Duration
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindTrack,
		Title:      v.Title,
		Subtitle:   subtitle,
		ImageURL:   imageURL,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderYouTube,
			ExternalID: v.VideoID,
			URL:        "https://music.youtube.com/watch?v=" + v.VideoID,
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
