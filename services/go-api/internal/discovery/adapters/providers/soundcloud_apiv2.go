package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"

	"golang.org/x/sync/singleflight"
)

// SoundCloudAPIAdapter searches SoundCloud through its internal api-v2 JSON
// backend — the same one the website calls — instead of shelling out to yt-dlp.
// It is a Track-only coverage provider: SoundCloud is where the unreleased /
// leaked / underground long tail lives, so this exists to widen the candidate
// set, not to feed ranking. Compared with the yt-dlp adapter it adds depth
// (paginated, not just 5), richer metadata (genre, playback/likes/reposts,
// inline artwork), a resolve?url= entry point, and speed (HTTP, no subprocess).
//
// AIDEV-DECISION: the internal api-v2 is undocumented and against SoundCloud's
// ToS — accepted for self-hosted personal/family use (plan 005). Its only gate
// is a public client_id embedded in the site's JS, which rotates; resolution is
// automatic, and a yt-dlp fallback covers a resolution outage.
type SoundCloudAPIAdapter struct {
	client   *http.Client
	resolver *clientIDResolver
	fallback searchFallback
	baseURL  string // api-v2 base; overridable in tests
}

// searchFallback is the subset of a SearchProvider this adapter falls back to
// (the yt-dlp adapter) when client_id resolution or the api-v2 call fails.
type searchFallback interface {
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

const (
	scAPIBaseURL    = "https://api-v2.soundcloud.com"
	scSearchLimit   = 20
	scMaxResults    = 40
	scSearchTimeout = 3 * time.Second
	scUserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

func NewSoundCloudAPIAdapter(client *http.Client, fallback searchFallback) *SoundCloudAPIAdapter {
	return &SoundCloudAPIAdapter{
		client:   client,
		resolver: newClientIDResolver(client),
		fallback: fallback,
		baseURL:  scAPIBaseURL,
	}
}

func (a *SoundCloudAPIAdapter) Name() domain.ProviderName { return domain.ProviderSoundCloud }

func (a *SoundCloudAPIAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true, // typed playlists (set_type album/ep/single)
		domain.ResultKindArtist: true, // users
	}
}

// SearchTimeout gives the HTTP client room for a cold-start client_id resolve
// plus a second page, while staying well under the yt-dlp adapter's 5s — the
// speed win the direct client buys.
func (a *SoundCloudAPIAdapter) SearchTimeout() time.Duration { return scSearchTimeout }

func (a *SoundCloudAPIAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult
	var firstErr error

	if kinds[domain.ResultKindTrack] {
		tracks, err := a.searchTracks(ctx, query)
		switch {
		case err == nil:
			results = append(results, tracks...)
		case a.fallback != nil && ctx.Err() == nil:
			// Fall back to yt-dlp (tracks only) when there's budget left — a
			// cancelled/expired context would fail the fallback too, just slower.
			slog.WarnContext(ctx, "soundcloud.apiv2_fallback", "query", query, "error", err)
			if fb, ferr := a.fallback.Search(ctx, query, kinds); ferr == nil {
				results = append(results, fb...)
			} else {
				firstErr = errors.Join(firstErr, err)
			}
		default:
			firstErr = errors.Join(firstErr, err)
		}
	}

	if kinds[domain.ResultKindAlbum] {
		if albums, err := a.searchAlbums(ctx, query); err != nil {
			firstErr = errors.Join(firstErr, err)
		} else {
			results = append(results, albums...)
		}
	}

	if kinds[domain.ResultKindArtist] {
		if artists, err := a.searchArtists(ctx, query); err != nil {
			firstErr = errors.Join(firstErr, err)
		} else {
			results = append(results, artists...)
		}
	}

	// Surface an error only when nothing came back; a partial mix still ships.
	if len(results) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

// searchTracks resolves a client_id, paginates the api-v2 track search, and
// retries once with a fresh client_id on an auth failure (the rotation case).
func (a *SoundCloudAPIAdapter) searchTracks(ctx context.Context, query string) ([]domain.SearchResult, error) {
	id, err := a.resolver.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve client_id: %w", err)
	}

	results, status, err := a.doSearch(ctx, id, query)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate()
		id, err = a.resolver.get(ctx)
		if err != nil {
			return nil, fmt.Errorf("re-resolve client_id: %w", err)
		}
		results, _, err = a.doSearch(ctx, id, query)
	}
	if err != nil {
		return nil, err
	}
	return results, nil
}

// doSearch walks next_href pages until scMaxResults or no further page. Once the
// first page has landed, a later-page error returns the partial set rather than
// failing the whole search — depth is best-effort, presence is not.
func (a *SoundCloudAPIAdapter) doSearch(ctx context.Context, clientID, query string) ([]domain.SearchResult, int, error) {
	results := make([]domain.SearchResult, 0, scMaxResults)
	next := fmt.Sprintf(
		"%s/search/tracks?q=%s&client_id=%s&limit=%d&offset=0",
		a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
	)

	for next != "" && len(results) < scMaxResults {
		if ctx.Err() != nil {
			break
		}
		page, nextHref, status, err := a.fetchSearchPage(ctx, next)
		if err != nil {
			if len(results) > 0 {
				return results, status, nil
			}
			return nil, status, err
		}
		results = append(results, page...)
		next = appendClientID(nextHref, clientID)
	}
	return results, http.StatusOK, nil
}

func (a *SoundCloudAPIAdapter) fetchSearchPage(ctx context.Context, u string) (tracks []domain.SearchResult, nextHref string, status int, err error) {
	var body scSearchResponse
	status, err = a.getJSON(ctx, u, &body)
	if err != nil {
		return nil, "", status, err
	}

	tracks = make([]domain.SearchResult, 0, len(body.Collection))
	for _, t := range body.Collection {
		if r, ok := mapSoundCloudAPITrack(t); ok {
			tracks = append(tracks, r)
		}
	}
	return tracks, body.NextHref, status, nil
}

// searchAlbums fetches one page of api-v2 album results (typed playlists). One
// page is plenty — album/artist relevance lives at the head, so the deep
// pagination the track long tail needs would only add latency here.
func (a *SoundCloudAPIAdapter) searchAlbums(ctx context.Context, query string) ([]domain.SearchResult, error) {
	var out []domain.SearchResult
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf(
			"%s/search/albums?q=%s&client_id=%s&limit=%d",
			a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
		)
		var body scAlbumSearchResponse
		status, err := a.getJSON(ctx, u, &body)
		if err != nil {
			return status, err
		}
		out = make([]domain.SearchResult, 0, len(body.Collection))
		for _, al := range body.Collection {
			if r, ok := mapSoundCloudAPIAlbum(al); ok {
				out = append(out, r)
			}
		}
		return status, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// searchArtists fetches one page of api-v2 user results (SoundCloud's "artist").
func (a *SoundCloudAPIAdapter) searchArtists(ctx context.Context, query string) ([]domain.SearchResult, error) {
	var out []domain.SearchResult
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf(
			"%s/search/users?q=%s&client_id=%s&limit=%d",
			a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
		)
		var body scUserSearchResponse
		status, err := a.getJSON(ctx, u, &body)
		if err != nil {
			return status, err
		}
		out = make([]domain.SearchResult, 0, len(body.Collection))
		for _, u := range body.Collection {
			if r, ok := mapSoundCloudAPIUser(u); ok {
				out = append(out, r)
			}
		}
		return status, nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// resolveAndFetch runs fetch with a resolved client_id, re-resolving once on an
// auth failure (the rotation case). Single-request callers use it; the
// paginated track search inlines the same shape because pagination restarts on
// a re-resolve.
func (a *SoundCloudAPIAdapter) resolveAndFetch(ctx context.Context, fetch func(clientID string) (int, error)) error {
	id, err := a.resolver.get(ctx)
	if err != nil {
		return fmt.Errorf("resolve client_id: %w", err)
	}
	status, err := fetch(id)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate()
		id, err = a.resolver.get(ctx)
		if err != nil {
			return fmt.Errorf("re-resolve client_id: %w", err)
		}
		_, err = fetch(id)
	}
	return err
}

// Resolve implements ports.ArtworkResolver: it searches SoundCloud for the
// entity and returns the best match's artwork URL (empty on miss, so the chain
// continues). Placed last in the artwork chain, it only fires for entities the
// ID-based sources couldn't cover — the underground long tail where SoundCloud
// is the sole artwork source. Mirrors the Deezer/iTunes name-search resolvers.
func (a *SoundCloudAPIAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if strings.TrimSpace(title) == "" {
		return "", nil
	}
	query := title
	if subtitle != "" {
		query = subtitle + " " + title
	}

	var (
		results []domain.SearchResult
		err     error
	)
	switch kind {
	case domain.ResultKindArtist:
		results, err = a.searchArtists(ctx, query)
	case domain.ResultKindAlbum:
		results, err = a.searchAlbums(ctx, query)
	default:
		results, err = a.searchArtworkTracks(ctx, query)
	}
	if err != nil {
		return "", nil // swallow — the chain falls through to the next resolver
	}
	for _, r := range results {
		if r.ImageURL != "" {
			return r.ImageURL, nil
		}
	}
	return "", nil
}

// searchArtworkTracks is a single-page track search for the artwork resolver —
// it needs only the top hit's image, not the deep paginated set searchTracks
// builds for coverage.
func (a *SoundCloudAPIAdapter) searchArtworkTracks(ctx context.Context, query string) ([]domain.SearchResult, error) {
	var out []domain.SearchResult
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf(
			"%s/search/tracks?q=%s&client_id=%s&limit=%d&offset=0",
			a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
		)
		page, _, status, err := a.fetchSearchPage(ctx, u)
		out = page
		return status, err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ResolvePermalink turns a SoundCloud permalink into the track it points at, via
// the api-v2 resolve endpoint. It serves the "paste a sheet of links and import
// them" workflow (plan 005) and is a candidate feed for the acquisition path;
// it is not part of the SearchProvider fan-out.
func (a *SoundCloudAPIAdapter) ResolvePermalink(ctx context.Context, permalink string) (*domain.SearchResult, error) {
	id, err := a.resolver.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve client_id: %w", err)
	}

	result, status, err := a.doResolve(ctx, id, permalink)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate()
		id, err = a.resolver.get(ctx)
		if err != nil {
			return nil, fmt.Errorf("re-resolve client_id: %w", err)
		}
		result, _, err = a.doResolve(ctx, id, permalink)
	}
	return result, err
}

func (a *SoundCloudAPIAdapter) doResolve(ctx context.Context, clientID, permalink string) (*domain.SearchResult, int, error) {
	u := fmt.Sprintf(
		"%s/resolve?url=%s&client_id=%s",
		a.baseURL, url.QueryEscape(permalink), url.QueryEscape(clientID),
	)

	var t scAPITrack
	status, err := a.getJSON(ctx, u, &t)
	if err != nil {
		return nil, status, err
	}
	result, ok := mapSoundCloudAPITrack(t)
	if !ok {
		return nil, status, fmt.Errorf("resolve %q did not yield a track", permalink)
	}
	return &result, status, nil
}

// getJSON performs a GET and decodes a 200 body into dst, returning the HTTP
// status (for auth-retry decisions) alongside any error.
func (a *SoundCloudAPIAdapter) getJSON(ctx context.Context, u string, dst any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", scUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, fmt.Errorf("soundcloud api-v2: status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return resp.StatusCode, fmt.Errorf("decode response: %w", err)
	}
	return resp.StatusCode, nil
}

// scSearchResponse is one page of api-v2 /search/tracks.
type scSearchResponse struct {
	Collection []scAPITrack `json:"collection"`
	NextHref   string       `json:"next_href"`
}

// scAPITrack is the subset of an api-v2 track object we consume.
type scAPITrack struct {
	ID            int64  `json:"id"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	PermalinkURL  string `json:"permalink_url"`
	Duration      int64  `json:"duration"` // milliseconds
	Genre         string `json:"genre"`
	ArtworkURL    string `json:"artwork_url"`
	PlaybackCount int64  `json:"playback_count"`
	LikesCount    int64  `json:"likes_count"`
	RepostsCount  int64  `json:"reposts_count"`
	User          struct {
		Username string `json:"username"`
	} `json:"user"`
}

// mapSoundCloudAPITrack converts an api-v2 track to a domain result. The richer
// fields (genre, likes, reposts) ride in Extras as latent coverage/acquisition
// signal; only duration + playback_count match the keys the pipeline already
// reads, so ranking behaviour is unchanged versus the yt-dlp adapter.
func mapSoundCloudAPITrack(t scAPITrack) (domain.SearchResult, bool) {
	if t.ID == 0 || strings.TrimSpace(t.Title) == "" {
		return domain.SearchResult{}, false
	}
	if t.Kind != "" && t.Kind != "track" {
		return domain.SearchResult{}, false
	}

	extras := map[string]any{}
	if t.Duration > 0 {
		extras["duration"] = float64(t.Duration) / 1000.0 // ms → seconds (yt-dlp parity)
	}
	if t.PlaybackCount > 0 {
		extras["playback_count"] = t.PlaybackCount
	}
	if t.LikesCount > 0 {
		extras["likes_count"] = t.LikesCount
	}
	if t.RepostsCount > 0 {
		extras["reposts_count"] = t.RepostsCount
	}
	if g := strings.TrimSpace(t.Genre); g != "" {
		extras["genre"] = g
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindTrack,
		Title:      t.Title,
		Subtitle:   t.User.Username,
		ImageURL:   upgradeArtworkResolution(t.ArtworkURL),
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderSoundCloud,
			ExternalID: strconv.FormatInt(t.ID, 10),
			URL:        t.PermalinkURL,
		}},
		Extras: extras,
	}, true
}

// scAlbumSearchResponse is one page of api-v2 /search/albums.
type scAlbumSearchResponse struct {
	Collection []scAPIAlbum `json:"collection"`
}

// scAPIAlbum is an api-v2 album — a playlist with set_type album/ep/single.
type scAPIAlbum struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"` // "playlist"
	Title        string `json:"title"`
	PermalinkURL string `json:"permalink_url"`
	ArtworkURL   string `json:"artwork_url"`
	SetType      string `json:"set_type"` // album | ep | single
	Genre        string `json:"genre"`
	User         struct {
		Username string `json:"username"`
	} `json:"user"`
}

func mapSoundCloudAPIAlbum(a scAPIAlbum) (domain.SearchResult, bool) {
	if a.ID == 0 || strings.TrimSpace(a.Title) == "" {
		return domain.SearchResult{}, false
	}

	extras := map[string]any{}
	if st := strings.TrimSpace(a.SetType); st != "" {
		extras["record_type"] = st // matches the ytmusic/discogs album key
	}
	if g := strings.TrimSpace(a.Genre); g != "" {
		extras["genre"] = g
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindAlbum,
		Title:      a.Title,
		Subtitle:   a.User.Username, // album artist = uploader
		ImageURL:   upgradeArtworkResolution(a.ArtworkURL),
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderSoundCloud,
			ExternalID: strconv.FormatInt(a.ID, 10),
			URL:        a.PermalinkURL,
		}},
		Extras: extras,
	}, true
}

// scUserSearchResponse is one page of api-v2 /search/users.
type scUserSearchResponse struct {
	Collection []scAPIUser `json:"collection"`
}

// scAPIUser is an api-v2 user — SoundCloud's notion of an artist.
type scAPIUser struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"` // "user"
	Username     string `json:"username"`
	PermalinkURL string `json:"permalink_url"`
	AvatarURL    string `json:"avatar_url"`
}

func mapSoundCloudAPIUser(u scAPIUser) (domain.SearchResult, bool) {
	if u.ID == 0 || strings.TrimSpace(u.Username) == "" {
		return domain.SearchResult{}, false
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      u.Username,
		ImageURL:   upgradeArtworkResolution(u.AvatarURL),
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderSoundCloud,
			ExternalID: strconv.FormatInt(u.ID, 10),
			URL:        u.PermalinkURL,
		}},
		Extras: map[string]any{},
	}, true
}

// upgradeArtworkResolution swaps SoundCloud's default 100px "-large" artwork
// variant for the 500px one — same URL, sharper image. For tracks that exist
// only on SoundCloud this is the sole artwork source, so it is worth the bump.
func upgradeArtworkResolution(artworkURL string) string {
	if artworkURL == "" {
		return ""
	}
	return strings.Replace(artworkURL, "-large.", "-t500x500.", 1)
}

func appendClientID(href, clientID string) string {
	if href == "" {
		return ""
	}
	sep := "?"
	if strings.Contains(href, "?") {
		sep = "&"
	}
	return href + sep + "client_id=" + url.QueryEscape(clientID)
}

func isAuthStatus(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusForbidden
}

// --- client_id resolution -------------------------------------------------

// scAssetURLRe matches the JS bundle URLs the SoundCloud homepage loads; one of
// them embeds the public client_id the api-v2 backend requires. The homepage we
// fetch is the trust anchor, so we match any absolute /assets/*.js it lists and
// scan each for the key, rather than hard-coding the CDN host.
var scAssetURLRe = regexp.MustCompile(`https?://[^"' ]+/assets/[^"' ]+\.js`)

// scClientIDRe extracts the 32-char client_id from a JS bundle, tolerating the
// minified `client_id:"…"` and `client_id="…"` forms.
var scClientIDRe = regexp.MustCompile(`client_id\s*[:=]\s*"?([a-zA-Z0-9]{32})"?`)

const (
	scSiteURL      = "https://soundcloud.com/"
	scMaxBodyBytes = 16 << 20 // JS bundles are a few MB; cap defensively
)

// clientIDResolver fetches and caches the public client_id the api-v2 backend
// requires. Resolution scrapes the homepage's JS bundles once; the cached value
// is reused until an auth failure invalidates it (the rotation tax). Concurrent
// cold-start callers are collapsed with singleflight so a burst of searches
// triggers exactly one scrape.
type clientIDResolver struct {
	client  *http.Client
	siteURL string // overridable in tests
	sf      singleflight.Group
	mu      sync.Mutex
	cached  string
}

func newClientIDResolver(client *http.Client) *clientIDResolver {
	return &clientIDResolver{client: client, siteURL: scSiteURL}
}

func (r *clientIDResolver) get(ctx context.Context) (string, error) {
	r.mu.Lock()
	cached := r.cached
	r.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	v, err, _ := r.sf.Do("client_id", func() (any, error) {
		r.mu.Lock()
		existing := r.cached
		r.mu.Unlock()
		if existing != "" {
			return existing, nil
		}
		return r.resolve(ctx)
	})
	if err != nil {
		return "", err
	}

	id, _ := v.(string)
	if id == "" {
		return "", errors.New("soundcloud: resolved empty client_id")
	}
	r.mu.Lock()
	r.cached = id
	r.mu.Unlock()
	return id, nil
}

func (r *clientIDResolver) invalidate() {
	r.mu.Lock()
	r.cached = ""
	r.mu.Unlock()
}

func (r *clientIDResolver) resolve(ctx context.Context) (string, error) {
	html, err := r.fetchText(ctx, r.siteURL)
	if err != nil {
		return "", fmt.Errorf("fetch soundcloud home: %w", err)
	}

	assets := dedupePreserveOrder(scAssetURLRe.FindAllString(html, -1))
	if len(assets) == 0 {
		return "", errors.New("no asset bundles found on soundcloud home")
	}

	// The client_id lives in one of the later bundles, so scan from the end.
	for i := len(assets) - 1; i >= 0; i-- {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		body, err := r.fetchText(ctx, assets[i])
		if err != nil {
			continue
		}
		if m := scClientIDRe.FindStringSubmatch(body); m != nil {
			return m[1], nil
		}
	}
	return "", errors.New("client_id not found in any asset bundle")
}

func (r *clientIDResolver) fetchText(ctx context.Context, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", scUserAgent)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, scMaxBodyBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func dedupePreserveOrder(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
