package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
)

const ytmusicTimeout = 8 * time.Second

func ytmHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Timeout: ytmusicTimeout, Transport: transport}
}

func ytmSearchRetry(ctx context.Context, client *http.Client, query string, filter ytmFilter) (*ytmResult, error) {
	const attempts = 2
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res, err := ytmSearch(ctx, client, query, filter)
		if err == nil {
			return res, nil
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

type YouTubeMusicAdapter struct {
	client *http.Client
}

func NewYouTubeMusicAdapter(transport http.RoundTripper) *YouTubeMusicAdapter {
	return &YouTubeMusicAdapter{client: ytmHTTPClient(transport)}
}

func (a *YouTubeMusicAdapter) Name() domain.ProviderName { return domain.ProviderYouTube }

func (a *YouTubeMusicAdapter) SearchTimeout() time.Duration { return 3 * time.Second }

func (a *YouTubeMusicAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *YouTubeMusicAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	result, err := ytmSearchRetry(ctx, a.client, query, ytmNoFilter)
	if err != nil {
		return nil, fmt.Errorf("ytmusic search: %w", err)
	}

	var results []domain.SearchResult

	if kinds[domain.ResultKindTrack] {
		for _, t := range result.Tracks {
			results = append(results, mapYTMusicTrack(t))
		}
		for _, v := range result.Videos {
			results = append(results, mapYTMusicVideo(v))
		}
	}
	if kinds[domain.ResultKindAlbum] {
		for _, al := range result.Albums {
			results = append(results, mapYTMusicAlbum(al))
		}
	}
	if kinds[domain.ResultKindArtist] {
		for _, ar := range result.Artists {
			results = append(results, mapYTMusicArtist(ar))
		}
	}

	return results, nil
}

func (a *YouTubeMusicAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	result, err := ytmSearchRetry(ctx, a.client, artistName, ytmAlbumFilter)
	if err != nil {
		return nil, fmt.Errorf("ytmusic album search: %w", err)
	}

	var results []domain.SearchResult
	for _, al := range result.Albums {
		artistMatch := false
		for _, artist := range al.Artists {
			if strings.EqualFold(artist.Name, artistName) {
				artistMatch = true
				break
			}
		}
		if !artistMatch {
			continue
		}
		results = append(results, mapYTMusicAlbum(al))
	}

	if len(result.Albums) > 0 && len(results) == 0 {
		slog.DebugContext(ctx, "ytmusic.no_artist_match",
			"artist", artistName,
			"albums_found", len(result.Albums),
		)
	}

	return results, nil
}

func mapYTMusicTrack(t *ytmTrack) domain.SearchResult {
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
	if t.IsExplicit {
		extras["explicit"] = true
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, t.Title, subtitle, imageURL,
		domain.SourceRef{Provider: domain.ProviderYouTube, ExternalID: t.VideoID, URL: "https://music.youtube.com/watch?v=" + t.VideoID},
		extras)
	r.Album = t.Album.Name
	r.Duration = t.Duration
	return r
}

func mapYTMusicVideo(v *ytmVideo) domain.SearchResult {
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

	r := domain.NewProviderResult(domain.ResultKindTrack, v.Title, subtitle, imageURL,
		domain.SourceRef{Provider: domain.ProviderYouTube, ExternalID: v.VideoID, URL: "https://music.youtube.com/watch?v=" + v.VideoID},
		extras)
	r.Duration = v.Duration
	return r
}

func mapYTMusicAlbum(a *ytmAlbum) domain.SearchResult {
	var subtitle string
	if len(a.Artists) > 0 {
		subtitle = a.Artists[0].Name
	}
	var imageURL string
	if len(a.Thumbnails) > 0 {
		imageURL = a.Thumbnails[len(a.Thumbnails)-1].URL
	}
	extras := make(map[string]any)
	if a.Type != "" {
		extras["record_type"] = a.Type
	}
	if a.IsExplicit {
		extras["explicit"] = true
	}

	r := domain.NewProviderResult(domain.ResultKindAlbum, a.Title, subtitle, imageURL,
		domain.SourceRef{Provider: domain.ProviderYouTube, ExternalID: a.BrowseID, URL: "https://music.youtube.com/browse/" + a.BrowseID},
		extras)
	if y, err := strconv.Atoi(strings.TrimSpace(a.Year)); err == nil && y > 0 {
		r.Year = y
	}
	return r
}

const ytArtworkHeroSize = 1000

var ytThumbSizeRe = regexp.MustCompile(`w\d+-h\d+`)

type YouTubeMusicArtworkResolver struct {
	client *http.Client
}

func NewYouTubeMusicArtworkResolver(transport http.RoundTripper) *YouTubeMusicArtworkResolver {
	return &YouTubeMusicArtworkResolver{client: ytmHTTPClient(transport)}
}

func (a *YouTubeMusicArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if kind != domain.ResultKindArtist || title == "" {
		return "", nil
	}
	result, err := ytmSearchRetry(ctx, a.client, title, ytmArtistFilter)
	if err != nil {
		return "", nil
	}
	url := pickArtistArtwork(result.Artists, title, ytArtworkHeroSize)
	if url != "" {
		slog.DebugContext(ctx, "ytmusic.artwork_resolved", "title", title)
	}
	return url, nil
}

func pickArtistArtwork(artists []*ytmArtistItem, name string, size int) string {
	var fallback string
	for _, artist := range artists {
		url := largestYTThumbnail(artist.Thumbnails)
		if url == "" {
			continue
		}
		if strings.EqualFold(artist.Artist, name) {
			return resizeYTThumbnail(url, size)
		}
		if fallback == "" {
			fallback = url
		}
	}
	if fallback == "" {
		return ""
	}
	return resizeYTThumbnail(fallback, size)
}

func largestYTThumbnail(thumbs []ytmThumbnail) string {
	if len(thumbs) == 0 {
		return ""
	}
	return thumbs[len(thumbs)-1].URL
}

func resizeYTThumbnail(url string, size int) string {
	if !ytThumbSizeRe.MatchString(url) {
		return url
	}
	return ytThumbSizeRe.ReplaceAllString(url, fmt.Sprintf("w%d-h%d", size, size))
}

func mapYTMusicArtist(a *ytmArtistItem) domain.SearchResult {
	var imageURL string
	if len(a.Thumbnails) > 0 {
		imageURL = a.Thumbnails[len(a.Thumbnails)-1].URL
	}

	return domain.NewProviderResult(domain.ResultKindArtist, a.Artist, "", imageURL,
		domain.SourceRef{Provider: domain.ProviderYouTube, ExternalID: a.BrowseID, URL: "https://music.youtube.com/channel/" + a.BrowseID},
		nil)
}

func (*YouTubeMusicArtworkResolver) ArtworkSource() string { return "ytmusic" }
