package ytmusic

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/search/internal/adapters/providers/providersearch"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/backoff"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/random"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ytmusicTimeout = 8 * time.Second

var ytmChallengeBackoff = backoff.MustCapped(250*time.Millisecond, 250*time.Millisecond)

func ytmHTTPClient(transport http.RoundTripper) *http.Client {
	return outbound.MustNewClient(ytmusicTimeout, transport)
}

var (
	ErrSleeperRequired = errors.New("ytmusic: the adapter needs a sleeper")
	ErrJitterRequired  = errors.New("ytmusic: the adapter needs a jitter source")
)

type challengePause struct {
	sleep  clock.Sleeper
	jitter random.Unit
}

func newChallengePause(constructor string, sleep clock.Sleeper, jitter random.Unit) (challengePause, error) {
	switch {
	case sleep == nil:
		return challengePause{}, fmt.Errorf("%s: %w", constructor, ErrSleeperRequired)
	case jitter == nil:
		return challengePause{}, fmt.Errorf("%s: %w", constructor, ErrJitterRequired)
	}
	return challengePause{sleep: sleep, jitter: jitter}, nil
}

func (p challengePause) wait(ctx context.Context) error {
	return p.sleep(ctx, ytmChallengeBackoff.DelayAt(1, p.jitter()))
}

func ytmSearchRetry(ctx context.Context, client *http.Client, pause challengePause, query string, filter ytmFilter) (*ytmResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res, err := ytmSearch(ctx, client, query, filter)
	if !isAuthChallenge(err) {
		return res, err
	}
	if err := pause.wait(ctx); err != nil {
		return nil, err
	}
	return ytmSearch(ctx, client, query, filter)
}

type upstreamStatusCoder interface {
	UpstreamStatus() int
}

func isAuthChallenge(err error) bool {
	var status upstreamStatusCoder
	return errors.As(err, &status) && outbound.IsAuthStatus(status.UpstreamStatus())
}

type YouTubeMusicAdapter struct {
	client *http.Client
	pause  challengePause
}

func NewYouTubeMusicAdapter(transport http.RoundTripper, sleep clock.Sleeper, jitter random.Unit) (*YouTubeMusicAdapter, error) {
	pause, err := newChallengePause("NewYouTubeMusicAdapter", sleep, jitter)
	if err != nil {
		return nil, err
	}
	return &YouTubeMusicAdapter{client: ytmHTTPClient(transport), pause: pause}, nil
}

func MustNewYouTubeMusicAdapter(transport http.RoundTripper, sleep clock.Sleeper, jitter random.Unit) *YouTubeMusicAdapter {
	adapter, err := NewYouTubeMusicAdapter(transport, sleep, jitter)
	if err != nil {
		panic(err)
	}
	return adapter
}

func (a *YouTubeMusicAdapter) Name() domain.ProviderName { return domain.ProviderYouTube }

func (a *YouTubeMusicAdapter) SearchTimeout() time.Duration { return 3 * time.Second }

func (a *YouTubeMusicAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return providersearch.AllSearchKinds()
}

func (a *YouTubeMusicAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	result, err := ytmSearchRetry(ctx, a.client, a.pause, query, ytmNoFilter)
	if errors.Is(err, errYTMUnexpectedShape) {
		return nil, nil
	}
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
	result, err := ytmSearchRetry(ctx, a.client, a.pause, artistName, ytmAlbumFilter)
	if errors.Is(err, errYTMUnexpectedShape) {
		return nil, nil
	}
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
	if t.Album.Name != "" {
		extras[domain.ExtraAlbum] = t.Album.Name
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, t.Title, subtitle, imageURL,
		domain.ProviderLink{Provider: domain.ProviderYouTube, ExternalID: t.VideoID, URL: "https://music.youtube.com/watch?v=" + t.VideoID},
		extras)
	r.Album = t.Album.Name
	r.Duration = t.Duration
	if t.IsExplicit {
		r.Explicit = domain.BoolPtr(true)
	}
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

	r := domain.NewProviderResult(domain.ResultKindTrack, v.Title, subtitle, imageURL,
		domain.ProviderLink{Provider: domain.ProviderYouTube, ExternalID: v.VideoID, URL: "https://music.youtube.com/watch?v=" + v.VideoID},
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

	r := domain.NewProviderResult(domain.ResultKindAlbum, a.Title, subtitle, imageURL,
		domain.ProviderLink{Provider: domain.ProviderYouTube, ExternalID: a.BrowseID, URL: "https://music.youtube.com/browse/" + a.BrowseID},
		extras)
	r.RecordType = domain.RecordType(a.Type)
	if a.IsExplicit {
		r.Explicit = domain.BoolPtr(true)
	}
	if y, err := strconv.Atoi(strings.TrimSpace(a.Year)); err == nil && y > 0 {
		r.Year = y
	}
	return r
}

const ytArtworkHeroSize = 1000

var ytThumbSizeRe = regexp.MustCompile(`w\d+-h\d+`)

type YouTubeMusicArtworkLookup struct {
	client *http.Client
	pause  challengePause
}

func NewYouTubeMusicArtworkLookup(transport http.RoundTripper, sleep clock.Sleeper, jitter random.Unit) (*YouTubeMusicArtworkLookup, error) {
	pause, err := newChallengePause("NewYouTubeMusicArtworkLookup", sleep, jitter)
	if err != nil {
		return nil, err
	}
	return &YouTubeMusicArtworkLookup{client: ytmHTTPClient(transport), pause: pause}, nil
}

func MustNewYouTubeMusicArtworkLookup(transport http.RoundTripper, sleep clock.Sleeper, jitter random.Unit) *YouTubeMusicArtworkLookup {
	lookup, err := NewYouTubeMusicArtworkLookup(transport, sleep, jitter)
	if err != nil {
		panic(err)
	}
	return lookup
}

func (a *YouTubeMusicArtworkLookup) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if kind != domain.ResultKindArtist || title == "" {
		return "", nil
	}
	result, err := ytmSearchRetry(ctx, a.client, a.pause, title, ytmArtistFilter)
	if err != nil {
		return providerrequests.ArtworkFailure(a.ArtworkSource(), err)
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
		domain.ProviderLink{Provider: domain.ProviderYouTube, ExternalID: a.BrowseID, URL: "https://music.youtube.com/channel/" + a.BrowseID},
		nil)
}

func (*YouTubeMusicArtworkLookup) ArtworkSource() domain.ProviderKey {
	return domain.ProviderKeyYTMusic
}
