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

// ytmusicTimeout bounds every YouTube Music call. The keyless internal endpoint
// has no SLA, so a slow/hung request must not block a search indefinitely.
const ytmusicTimeout = 8 * time.Second

// ytmHTTPClient builds the timeout-bounded client the adapter and the artwork
// resolver share-shape. transport is injected (nil → default transport) so the
// deterministic eval can record/replay YouTube Music like every other provider.
func ytmHTTPClient(transport http.RoundTripper) *http.Client {
	return &http.Client{Timeout: ytmusicTimeout, Transport: transport}
}

// ytmSearchRetry runs a search with one retry on a transient error — notably the
// intermittent HTTP 403 rate-limit, whose HTML body surfaces as a JSON decode
// error — while respecting the caller's context.
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

// NewYouTubeMusicAdapter builds the YouTube Music search adapter. transport is
// injected into the adapter's own HTTP client (nil → default transport), so
// offline tooling can record/replay YouTube Music like every other provider.
func NewYouTubeMusicAdapter(transport http.RoundTripper) *YouTubeMusicAdapter {
	return &YouTubeMusicAdapter{client: ytmHTTPClient(transport)}
}

func (a *YouTubeMusicAdapter) Name() domain.ProviderName { return domain.ProviderYouTube }

// SearchTimeout gives YouTube Music a larger budget than the default fan-out
// timeout so the adapter has room to retry the intermittent rate-limit (HTTP
// 403) it returns under bursty load.
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
		// AIDEV-NOTE: Coverage fix (plan 003 U6, Pattern C). YouTube Music
		// classifies many obscure/underground recordings as videos
		// (MUSIC_VIDEO_TYPE_OMV/UGC), which our parser routes to result.Videos —
		// not result.Tracks. Dropping them left the exact track absent from the
		// candidate set, so the ranker substituted the artist's hit. Mapping
		// videos as tracks recovers the recording; the categorical merge dedups
		// any video that duplicates an official track.
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

func (a *YouTubeMusicAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	result, err := ytmSearchRetry(ctx, a.client, artistName, ytmTrackFilter)
	if err != nil {
		return nil, fmt.Errorf("ytmusic track search: %w", err)
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

	r := domain.NewProviderResult(domain.ResultKindTrack, t.Title, subtitle, imageURL,
		domain.SourceRef{Provider: domain.ProviderYouTube, ExternalID: t.VideoID, URL: "https://music.youtube.com/watch?v=" + t.VideoID},
		extras)
	r.Album = t.Album.Name
	r.Duration = t.Duration
	return r
}

// mapYTMusicVideo maps a YouTube Music video result to a track. Used by the
// Pattern-C coverage fix: obscure recordings YT Music classifies as videos are
// still the playable track the user wants.
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

	r := domain.NewProviderResult(domain.ResultKindAlbum, a.Title, subtitle, imageURL,
		domain.SourceRef{Provider: domain.ProviderYouTube, ExternalID: a.BrowseID, URL: "https://music.youtube.com/browse/" + a.BrowseID},
		extras)
	// YT Music carries the year as a display string; parse it into the typed field.
	if y, err := strconv.Atoi(strings.TrimSpace(a.Year)); err == nil && y > 0 {
		r.Year = y
	}
	return r
}

// ytArtworkHeroSize is the square dimension the artist-artwork hero is resized
// to. YouTube Music thumbnails are URL-resizable (the `=wN-hN` suffix); the raw
// master (`=s0`) can be many MB (an artist photo probed at 13.9MB), so 1000px is
// the hero sweet spot (~130KB, live-verified) — comfortably above Deezer's 1000
// and Discogs's 600 artist fallbacks.
const ytArtworkHeroSize = 1000

// ytThumbSizeRe matches the `w<digits>-h<digits>` segment of a Google-hosted
// YouTube Music thumbnail URL, preserving any trailing crop flags (e.g. the
// artist `-p-` smart-crop) when rewritten.
var ytThumbSizeRe = regexp.MustCompile(`w\d+-h\d+`)

// YouTubeMusicArtworkResolver resolves artist artwork from the keyless YouTube
// Music internal API. It earns its place in the chain because (a) iTunes — our
// highest-res keyless artwork source — carries no artist images at all, and
// (b) the official YouTube Data API resolver is key-gated and quota-crippled
// (search.list costs 100 of 10k daily units → ~100 lookups/day). The internal
// API returns real, high-res artist photos with no key and no quota.
//
// AIDEV-NOTE: artist-only by design. Album/track artwork is already well covered
// by the ID-keyed sources (CAA 1200 / Deezer 1000 / iTunes 1500-from-3000); YT
// Music adds no album ceiling above those, only artist images they lack.
type YouTubeMusicArtworkResolver struct {
	client *http.Client
}

// NewYouTubeMusicArtworkResolver builds the keyless YouTube Music artist-artwork
// resolver. transport is injected into the resolver's own HTTP client (nil →
// default transport).
func NewYouTubeMusicArtworkResolver(transport http.RoundTripper) *YouTubeMusicArtworkResolver {
	return &YouTubeMusicArtworkResolver{client: ytmHTTPClient(transport)}
}

// Resolve returns a high-res artist image URL, or "" so the chain falls through.
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

// pickArtistArtwork chooses the best artist image from a search result and
// rewrites it to `size`. It prefers an exact (case-insensitive) name match to
// avoid wrong-artist images, falling back to the top result — which the caller
// searched by this exact name, so a name-matched photo beats no photo (the
// chain's fallback philosophy). Pure (no network) for testability.
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

// largestYTThumbnail returns the URL of the highest-resolution thumbnail (the
// API orders them ascending by size).
func largestYTThumbnail(thumbs []ytmThumbnail) string {
	if len(thumbs) == 0 {
		return ""
	}
	return thumbs[len(thumbs)-1].URL
}

// resizeYTThumbnail rewrites a YouTube Music thumbnail URL to a square `size`,
// preserving any crop flags. Returns the URL unchanged if it carries no
// recognizable `wN-hN` size segment.
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
