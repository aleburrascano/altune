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

type SoundCloudAPIAdapter struct {
	client   *http.Client
	resolver *clientIDResolver
	fallback searchFallback
	baseURL  string
}

type searchFallback interface {
	Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

const (
	scAPIBaseURL         = "https://api-v2.soundcloud.com"
	scSearchLimit        = 20
	scMaxResults         = 40
	scArtistContentLimit = 50
	scRelatedLimit       = 20
	scSearchTimeout      = 3 * time.Second
	scUserAgent          = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
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
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

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

	if len(results) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return results, nil
}

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

const scMaxSearchPages = 5

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

func (a *SoundCloudAPIAdapter) searchAlbums(ctx context.Context, query string) ([]domain.SearchResult, error) {
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/search/albums?q=%s&client_id=%s&limit=%d",
			a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
		)
	}, mapSoundCloudAPIAlbum)
}

func (a *SoundCloudAPIAdapter) searchArtists(ctx context.Context, query string) ([]domain.SearchResult, error) {
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/search/users?q=%s&client_id=%s&limit=%d",
			a.baseURL, url.QueryEscape(query), url.QueryEscape(clientID), scSearchLimit,
		)
	}, mapSoundCloudAPIUser)
}

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
		return "", nil
	}
	for _, r := range results {
		if r.ImageURL != "" {
			return r.ImageURL, nil
		}
	}
	return "", nil
}

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

func (a *SoundCloudAPIAdapter) GetAlbumTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	tracks, err := a.fetchPlaylistTracks(ctx, externalID)
	if err == nil && len(tracks) > 0 {
		return tracks, nil
	}
	if err != nil && !errors.Is(err, errSCNotFound) {
		return nil, err
	}
	return a.fetchSingleAsTracklist(ctx, externalID)
}

var errSCNotFound = errors.New("soundcloud: not found")

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
		return albums, nil
	}
	return append(albums, singles...), nil
}

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

func (a *SoundCloudAPIAdapter) GetRelatedTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/tracks/%s/related?client_id=%s&limit=%d",
			a.baseURL, url.PathEscape(externalID), url.QueryEscape(clientID), scRelatedLimit,
		)
	}, mapSoundCloudAPITrack)
}

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

type scSearchResponse struct {
	Collection []scAPITrack `json:"collection"`
	NextHref   string       `json:"next_href"`
}

type scAPITrack struct {
	ID                int64  `json:"id"`
	Kind              string `json:"kind"`
	Title             string `json:"title"`
	PermalinkURL      string `json:"permalink_url"`
	DurationMs        int64  `json:"duration"`
	Genre             string `json:"genre"`
	ArtworkURL        string `json:"artwork_url"`
	PlaybackCount     int64  `json:"playback_count"`
	LikesCount        int64  `json:"likes_count"`
	RepostsCount      int64  `json:"reposts_count"`
	ReleaseDate       string `json:"release_date"`
	DisplayDate       string `json:"display_date"`
	CreatedAt         string `json:"created_at"`
	PublisherMetadata struct {
		ISRC       string `json:"isrc"`
		AlbumTitle string `json:"album_title"`
	} `json:"publisher_metadata"`
	User struct {
		Username string `json:"username"`
	} `json:"user"`
}

func mapSoundCloudAPITrack(t scAPITrack) (domain.SearchResult, bool) {
	if t.ID == 0 || strings.TrimSpace(t.Title) == "" {
		return domain.SearchResult{}, false
	}
	if t.Kind != "" && t.Kind != "track" {
		return domain.SearchResult{}, false
	}

	extras := map[string]any{}
	if t.DurationMs > 0 {
		extras["duration"] = float64(t.DurationMs) / 1000.0
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
	r.ISRC = strings.TrimSpace(t.PublisherMetadata.ISRC)
	r.Album = strings.TrimSpace(t.PublisherMetadata.AlbumTitle)
	r.ReleaseDate = scBestReleaseDate(t.ReleaseDate, t.DisplayDate, t.CreatedAt)
	if t.DurationMs > 0 {
		r.Duration = int(t.DurationMs / 1000)
	}
	return r, true
}

type scAPIAlbum struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
	Title        string `json:"title"`
	PermalinkURL string `json:"permalink_url"`
	ArtworkURL   string `json:"artwork_url"`
	SetType      string `json:"set_type"`
	Genre        string `json:"genre"`
	TrackCount   int    `json:"track_count"`
	ReleaseDate  string `json:"release_date"`
	DisplayDate  string `json:"display_date"`
	CreatedAt    string `json:"created_at"`
	User         struct {
		Username string `json:"username"`
	} `json:"user"`
	Tracks []scPlaylistTrackRef `json:"tracks"`
}

type scPlaylistTrackRef struct {
	ID int64 `json:"id"`
}

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
		extras["record_type"] = st
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

type scAPIUser struct {
	ID           int64  `json:"id"`
	Kind         string `json:"kind"`
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
