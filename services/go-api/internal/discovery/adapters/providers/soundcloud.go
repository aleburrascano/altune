package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
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
	scAPIBaseURL  = "https://api-v2.soundcloud.com"
	scSearchLimit = 20
	scMaxResults  = 40
	// scArtistContentLimit bounds a single artist's toptracks/albums fetch; the
	// GetArtistContentService truncates further per request.
	scArtistContentLimit = 50
	// scRelatedLimit bounds a single /tracks/{id}/related fetch; the
	// GetRelatedTracksService truncates further per request.
	scRelatedLimit  = 20
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
		a.resolver.invalidate(id)
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

// scMaxSearchPages caps the next_href walk — a server that keeps returning
// empty pages with a next_href would otherwise spin until the ctx deadline.
const scMaxSearchPages = 5

// doSearch walks next_href pages until scMaxResults or no further page. Once the
// first page has landed, a later-page error returns the partial set rather than
// failing the whole search — depth is best-effort, presence is not.
func (a *SoundCloudAPIAdapter) doSearch(ctx context.Context, clientID, query string) ([]domain.SearchResult, int, error) {
	results := make([]domain.SearchResult, 0, scMaxResults)
	next := fmt.Sprintf(
		"%s/search/tracks?q=%s&client_id=%s&limit=%d&offset=0",
		a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
	)

	for page := 0; next != "" && len(results) < scMaxResults && page < scMaxSearchPages; page++ {
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
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/search/albums?q=%s&client_id=%s&limit=%d",
			a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
		)
	}, mapSoundCloudAPIAlbum)
}

// searchArtists fetches one page of api-v2 user results (SoundCloud's "artist").
func (a *SoundCloudAPIAdapter) searchArtists(ctx context.Context, query string) ([]domain.SearchResult, error) {
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/search/users?q=%s&client_id=%s&limit=%d",
			a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
		)
	}, mapSoundCloudAPIUser)
}

// ResolveArtistID implements ports.ArtistIDResolver: it maps an artist name to
// SoundCloud's numeric user id via the top user-search hit, so the identity-first
// content fan-out can reach SoundCloud even though MusicBrainz never bridges an
// SC id. Returns ok=false on no match or error (the provider then sits out).
func (a *SoundCloudAPIAdapter) ResolveArtistID(ctx context.Context, name string) (string, bool) {
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	results, err := a.searchArtists(ctx, name)
	if err != nil || len(results) == 0 {
		return "", false
	}
	for _, r := range results {
		if len(r.Sources) > 0 && r.Sources[0].ExternalID != "" {
			return r.Sources[0].ExternalID, true
		}
	}
	return "", false
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
		a.resolver.invalidate(id)
		id, err = a.resolver.get(ctx)
		if err != nil {
			return fmt.Errorf("re-resolve client_id: %w", err)
		}
		_, err = fetch(id)
	}
	return err
}

// scFetchList fetches one api-v2 collection page (client_id auth-retry included)
// and maps each item to a SearchResult. urlFn builds the request URL from the
// resolved client_id; mapFn maps one decoded item and drops the ones it rejects.
// It is the shared shape behind every single-page SoundCloud search/content
// endpoint — only the URL, the item type, and the mapper vary.
func scFetchList[T any](
	ctx context.Context,
	a *SoundCloudAPIAdapter,
	urlFn func(clientID string) string,
	mapFn func(T) (domain.SearchResult, bool),
) ([]domain.SearchResult, error) {
	var out []domain.SearchResult
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		var body struct {
			Collection []T `json:"collection"`
		}
		status, err := a.getJSON(ctx, urlFn(clientID), &body)
		if err != nil {
			return status, err
		}
		out = make([]domain.SearchResult, 0, len(body.Collection))
		for _, item := range body.Collection {
			if r, ok := mapFn(item); ok {
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
		a.resolver.invalidate(id)
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

// GetArtistTopTracks implements ports.ArtistContentProvider: an artist's most
// popular tracks. externalID is the SoundCloud numeric user id (or a profile
// handle when bridged from a MusicBrainz soundcloud url-relation).
func (a *SoundCloudAPIAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	userID, err := a.resolveUserID(ctx, externalID)
	if err != nil || userID == "" {
		return nil, err
	}
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/users/%s/toptracks?client_id=%s&limit=%d",
			a.baseURL, url.PathEscape(userID), url.QueryEscape(clientID), scArtistContentLimit,
		)
	}, mapSoundCloudAPITrack)
}

// GetAlbumTracks implements ports.AlbumContentProvider. A SoundCloud discography
// entry is either a playlist (album/EP — externalID is a playlist id) or a
// standalone single (externalID is a track id); the two id spaces overlap and
// aren't distinguishable up front, so try the playlist, then fall back to the
// single track. Without this a SoundCloud-sourced entry has no native tracklist
// and the album-tracks service falls back to a blind Deezer title search that
// returns a DIFFERENT album (a single "14 HAHAHA LOL" showed "REST IN BASS").
func (a *SoundCloudAPIAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	tracks, err := a.fetchPlaylistTracks(ctx, externalID)
	if err == nil && len(tracks) > 0 {
		return tracks, nil
	}
	// Fall through to the overlapping track-id namespace only on a definitive
	// 404 (the id names a track, not a playlist) or an empty playlist — a
	// transient failure (timeout, 5xx) must propagate, or it would resolve an
	// unrelated single-track "tracklist".
	if err != nil && !errors.Is(err, errSCNotFound) {
		return nil, err
	}
	return a.fetchSingleAsTracklist(ctx, externalID)
}

// errSCNotFound marks a definitive api-v2 404 — the id does not name that kind
// of resource — as opposed to a transient failure.
var errSCNotFound = errors.New("soundcloud: not found")

// fetchPlaylistTracks returns a SoundCloud playlist's member tracks (album/EP).
// A 404 (the id is a track, not a playlist) is wrapped in errSCNotFound so
// GetAlbumTracks can fall back to the single-track path; other errors propagate.
func (a *SoundCloudAPIAdapter) fetchPlaylistTracks(ctx context.Context, playlistID string) ([]domain.SearchResult, error) {
	var out []domain.SearchResult
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf("%s/playlists/%s?client_id=%s",
			a.baseURL, url.PathEscape(playlistID), url.QueryEscape(clientID))
		var pl struct {
			Tracks []scAPITrack `json:"tracks"`
		}
		status, err := a.getJSON(ctx, u, &pl)
		if err != nil {
			if status == http.StatusNotFound {
				return status, fmt.Errorf("playlist %s: %w", playlistID, errSCNotFound)
			}
			return status, err
		}
		out = make([]domain.SearchResult, 0, len(pl.Tracks))
		for _, t := range pl.Tracks {
			if r, ok := mapSoundCloudAPITrack(t); ok {
				out = append(out, r)
			}
		}
		return status, nil
	})
	return out, err
}

// fetchSingleAsTracklist returns a single track upload as a one-element tracklist
// (a SoundCloud single's "tracklist" is just itself).
func (a *SoundCloudAPIAdapter) fetchSingleAsTracklist(ctx context.Context, trackID string) ([]domain.SearchResult, error) {
	var out []domain.SearchResult
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf("%s/tracks/%s?client_id=%s",
			a.baseURL, url.PathEscape(trackID), url.QueryEscape(clientID))
		var t scAPITrack
		status, err := a.getJSON(ctx, u, &t)
		if err != nil {
			return status, err
		}
		if r, ok := mapSoundCloudAPITrack(t); ok {
			out = []domain.SearchResult{r}
		}
		return status, nil
	})
	return out, err
}

// GetArtistAlbums implements ports.ArtistContentProvider. externalID is the
// SoundCloud numeric user id (or a profile handle when bridged from a MusicBrainz
// soundcloud url-relation). The discography is the artist's typed playlists
// (album/ep/single) PLUS their standalone track uploads as singles — SoundCloud
// has no release objects, so a genuine single is just an upload not grouped into
// a playlist. Without the standalone half, SC-exclusive drops (the underground
// long tail, e.g. a rapper's latest SoundCloud single) never reach the discography.
func (a *SoundCloudAPIAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	userID, err := a.resolveUserID(ctx, externalID)
	if err != nil || userID == "" {
		return nil, err
	}
	albums, inPlaylist, err := a.fetchArtistPlaylists(ctx, userID)
	if err != nil {
		return nil, err
	}
	singles, err := a.fetchArtistStandaloneSingles(ctx, userID, inPlaylist)
	if err != nil {
		// Playlists already succeeded; degrade to albums-only rather than fail the
		// whole discography on the singles half.
		return albums, nil
	}
	return append(albums, singles...), nil
}

// resolveUserID maps an artist content id that is either a numeric SoundCloud
// user id or a profile handle (bridged from a MusicBrainz soundcloud url-relation,
// e.g. "che") into the numeric user id the content endpoints require. A numeric
// input passes through untouched (no extra request).
func (a *SoundCloudAPIAdapter) resolveUserID(ctx context.Context, ref string) (string, error) {
	if ref == "" || isAllDigits(ref) {
		return ref, nil
	}
	permalink := ref
	if !strings.HasPrefix(permalink, "http") {
		permalink = "https://soundcloud.com/" + ref
	}
	var userID string
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf("%s/resolve?url=%s&client_id=%s",
			a.baseURL, url.QueryEscape(permalink), url.QueryEscape(clientID))
		var user scAPIUser
		status, err := a.getJSON(ctx, u, &user)
		if err != nil {
			return status, err
		}
		if user.ID == 0 {
			return status, fmt.Errorf("resolve %q did not yield a user", permalink)
		}
		userID = strconv.FormatInt(user.ID, 10)
		return status, nil
	})
	return userID, err
}

// fetchArtistPlaylists fetches the artist's typed playlists (albums/EPs/singles)
// and, alongside the mapped albums, the set of every member track id — so the
// standalone-singles pass can exclude tracks that already belong to a playlist.
func (a *SoundCloudAPIAdapter) fetchArtistPlaylists(ctx context.Context, userID string) ([]domain.SearchResult, map[int64]bool, error) {
	var albums []domain.SearchResult
	inPlaylist := map[int64]bool{}
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf("%s/users/%s/albums?client_id=%s&limit=%d",
			a.baseURL, url.PathEscape(userID), url.QueryEscape(clientID), scArtistContentLimit)
		var body struct {
			Collection []scAPIAlbum `json:"collection"`
		}
		status, err := a.getJSON(ctx, u, &body)
		if err != nil {
			return status, err
		}
		albums = make([]domain.SearchResult, 0, len(body.Collection))
		for _, pl := range body.Collection {
			for _, tr := range pl.Tracks {
				if tr.ID != 0 {
					inPlaylist[tr.ID] = true
				}
			}
			if r, ok := mapSoundCloudAPIAlbum(pl); ok {
				albums = append(albums, r)
			}
		}
		return status, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return albums, inPlaylist, nil
}

// fetchArtistStandaloneSingles fetches the artist's uploads (newest-first) and
// maps each one NOT already in a playlist to a single. Returns at most the
// scArtistContentLimit most recent uploads.
func (a *SoundCloudAPIAdapter) fetchArtistStandaloneSingles(ctx context.Context, userID string, inPlaylist map[int64]bool) ([]domain.SearchResult, error) {
	var singles []domain.SearchResult
	err := a.resolveAndFetch(ctx, func(clientID string) (int, error) {
		u := fmt.Sprintf("%s/users/%s/tracks?client_id=%s&limit=%d",
			a.baseURL, url.PathEscape(userID), url.QueryEscape(clientID), scArtistContentLimit)
		var body struct {
			Collection []scAPITrack `json:"collection"`
		}
		status, err := a.getJSON(ctx, u, &body)
		if err != nil {
			return status, err
		}
		singles = make([]domain.SearchResult, 0, len(body.Collection))
		for _, t := range body.Collection {
			if inPlaylist[t.ID] {
				continue
			}
			if r, ok := mapSoundCloudStandaloneSingle(t); ok {
				singles = append(singles, r)
			}
		}
		return status, nil
	})
	if err != nil {
		return nil, err
	}
	return singles, nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// GetRelatedTracks implements ports.RelatedTracksProvider: SoundCloud's per-track
// recommendation set. externalID is the SoundCloud numeric track id, which a
// SoundCloud-sourced track result already carries in its SourceRef. Reuses the
// track mapper, so related items are ordinary track SearchResults.
func (a *SoundCloudAPIAdapter) GetRelatedTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/tracks/%s/related?client_id=%s&limit=%d",
			a.baseURL, url.PathEscape(externalID), url.QueryEscape(clientID), scRelatedLimit,
		)
	}, mapSoundCloudAPITrack)
}

// getJSON performs a GET and decodes a 200 body into dst, returning the HTTP
// status (for auth-retry decisions) alongside any error.
func (a *SoundCloudAPIAdapter) getJSON(ctx context.Context, u string, dst any) (int, error) {
	status, body, err := getBytes(ctx, a.client, u, withHeader("User-Agent", scUserAgent))
	if err != nil {
		return status, fmt.Errorf("soundcloud api-v2: %w", err)
	}
	if err := json.Unmarshal(body, dst); err != nil {
		return status, fmt.Errorf("decode response: %w", err)
	}
	return status, nil
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
	// Date fields (scBestReleaseDate order): tracks leave release_date null but
	// always carry display_date, so the detail screen still gets a year.
	ReleaseDate string `json:"release_date"`
	DisplayDate string `json:"display_date"`
	CreatedAt   string `json:"created_at"`
	// PublisherMetadata carries the distributor-supplied ISRC on officially
	// released uploads — the identity key that lets a SoundCloud track merge
	// with the same recording from Deezer/MusicBrainz (often null for pure
	// underground uploads, which is fine: it's additive).
	PublisherMetadata struct {
		ISRC       string `json:"isrc"`
		AlbumTitle string `json:"album_title"`
	} `json:"publisher_metadata"`
	User struct {
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
	if al := strings.TrimSpace(t.PublisherMetadata.AlbumTitle); al != "" {
		extras["album"] = al
	}

	r := domain.NewProviderResult(domain.ResultKindTrack, t.Title, t.User.Username, upgradeArtworkResolution(t.ArtworkURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(t.ID, 10), URL: t.PermalinkURL},
		extras)
	// ISRC lifts the track from EntityResolutionNone into the isrc merge tier —
	// SoundCloud tracks otherwise never merge with other providers (see merge.go).
	r.ISRC = strings.TrimSpace(t.PublisherMetadata.ISRC)
	r.Album = strings.TrimSpace(t.PublisherMetadata.AlbumTitle)
	r.ReleaseDate = scBestReleaseDate(t.ReleaseDate, t.DisplayDate, t.CreatedAt)
	if t.Duration > 0 {
		r.Duration = int(t.Duration / 1000)
	}
	return r, true
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
	TrackCount   int    `json:"track_count"`
	// Three date fields, in preference order (scBestReleaseDate): release_date is
	// the uploader-declared date (clean but often null), display_date is what
	// SoundCloud's own UI shows (always populated), created_at is the upload
	// timestamp. Feeding ReleaseDate gives the discography a year to display.
	ReleaseDate string `json:"release_date"`
	DisplayDate string `json:"display_date"`
	CreatedAt   string `json:"created_at"`
	User        struct {
		Username string `json:"username"`
	} `json:"user"`
	// Tracks carries each member track (full objects for the first page, id-only
	// stubs beyond), used to exclude playlist tracks from the standalone-singles
	// set so an EP track never also renders as a top-level single.
	Tracks []scPlaylistTrackRef `json:"tracks"`
}

// scPlaylistTrackRef is the minimal member-track reference we read from a playlist.
type scPlaylistTrackRef struct {
	ID int64 `json:"id"`
}

// scBestReleaseDate returns the first populated of release_date → display_date →
// created_at. Albums carry release_date; tracks leave it null but always have
// display_date, so this yields a usable date for both.
func scBestReleaseDate(releaseDate, displayDate, createdAt string) string {
	for _, d := range []string{releaseDate, displayDate, createdAt} {
		if s := strings.TrimSpace(d); s != "" {
			return s
		}
	}
	return ""
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

	r := domain.NewProviderResult(domain.ResultKindAlbum, a.Title, a.User.Username, upgradeArtworkResolution(a.ArtworkURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(a.ID, 10), URL: a.PermalinkURL},
		extras)
	r.TrackCount = a.TrackCount
	r.ReleaseDate = scBestReleaseDate(a.ReleaseDate, a.DisplayDate, a.CreatedAt)
	return r, true
}

// mapSoundCloudStandaloneSingle maps a standalone track upload to a discography
// single (an album-kind result with record_type=single, one track), the same
// shape Spotify/Apple/Deezer singles take, so it buckets correctly in the
// discography. Used only for uploads not already in one of the artist's playlists.
func mapSoundCloudStandaloneSingle(t scAPITrack) (domain.SearchResult, bool) {
	if t.ID == 0 || strings.TrimSpace(t.Title) == "" {
		return domain.SearchResult{}, false
	}
	if t.Kind != "" && t.Kind != "track" {
		return domain.SearchResult{}, false
	}
	extras := map[string]any{"record_type": "single"}
	if g := strings.TrimSpace(t.Genre); g != "" {
		extras["genre"] = g
	}
	r := domain.NewProviderResult(domain.ResultKindAlbum, t.Title, t.User.Username, upgradeArtworkResolution(t.ArtworkURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(t.ID, 10), URL: t.PermalinkURL},
		extras)
	r.TrackCount = 1
	r.ReleaseDate = scBestReleaseDate(t.ReleaseDate, t.DisplayDate, t.CreatedAt)
	return r, true
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

	return domain.NewProviderResult(domain.ResultKindArtist, u.Username, "", upgradeArtworkResolution(u.AvatarURL),
		domain.SourceRef{Provider: domain.ProviderSoundCloud, ExternalID: strconv.FormatInt(u.ID, 10), URL: u.PermalinkURL},
		nil), true
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

func (*SoundCloudAPIAdapter) ArtworkSource() string { return "soundcloud" }
