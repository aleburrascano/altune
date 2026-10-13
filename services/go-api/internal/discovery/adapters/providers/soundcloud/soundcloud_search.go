package soundcloud

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/adapters/providers/providersearch"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

func (a *SoundCloudAPIAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult
	attempted := 0
	var failed []domain.ResultKind
	var errs []error
	collect := func(kind domain.ResultKind, items []domain.SearchResult, err error) {
		attempted++
		if err != nil {
			failed = append(failed, kind)
			errs = append(errs, err)
			return
		}
		results = append(results, items...)
	}

	if kinds[domain.ResultKindTrack] {
		tracks, err := a.searchTracksOrFallback(ctx, query, kinds)
		collect(domain.ResultKindTrack, tracks, err)
	}
	if kinds[domain.ResultKindAlbum] {
		albums, err := a.searchAlbums(ctx, query)
		collect(domain.ResultKindAlbum, albums, err)
	}
	if kinds[domain.ResultKindArtist] {
		artists, err := a.searchArtists(ctx, query)
		collect(domain.ResultKindArtist, artists, err)
	}

	return results, providersearch.KindsOutcome(a.Name().String(), attempted, failed, errs)
}

func (a *SoundCloudAPIAdapter) searchTracksOrFallback(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	tracks, err := a.searchTracks(ctx, query)
	if err == nil || a.fallback == nil || ctx.Err() != nil {
		return tracks, err
	}
	slog.WarnContext(ctx, "soundcloud.apiv2_fallback",
		logging.SearchTextAttr(query), "error", redact.Secrets(logging.ScrubSearchErr(err, query)))
	fb, ferr := a.fallback.Search(ctx, query, kinds)
	if ferr != nil {
		return nil, err
	}
	return fb, nil
}

func (a *SoundCloudAPIAdapter) searchTracks(ctx context.Context, query string) ([]domain.SearchResult, error) {
	results, err := providerhttp.WithAuthRetry(ctx, a.resolver.CachedResolver,
		func(ctx context.Context, id string) ([]domain.SearchResult, int, error) {
			return a.doSearch(ctx, id, query)
		})
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

	for page := 0; ctx.Err() == nil && next != "" && len(results) < scMaxResults && page < scMaxSearchPages; page++ {
		page, nextHref, status, err := a.fetchSearchPage(ctx, next)
		if err != nil {
			if len(results) > 0 {
				return results, status, nil
			}
			return nil, status, err
		}
		results = append(results, page...)
		next = a.sameHostNextURL(nextHref, clientID)
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
		return providerhttp.ArtworkFailure(a.ArtworkSource(), err)
	}
	for _, r := range results {
		if r.ImageURL != "" && scArtworkMatches(kind, title, subtitle, r) {
			return r.ImageURL, nil
		}
	}
	return "", nil
}

func scArtworkMatches(kind domain.ResultKind, title, subtitle string, r domain.SearchResult) bool {
	if kind == domain.ResultKindArtist {
		return coversTokensOf(r.Title, title)
	}
	if !coversTokensOf(r.Title, title) {
		return false
	}
	return subtitle == "" || coversTokensOf(r.Title+" "+r.Subtitle, subtitle)
}

func coversTokensOf(haystack, needle string) bool {
	want := strings.Fields(textnorm.NormalizeForMatch(needle))
	if len(want) == 0 {
		return false
	}
	have := tokenSet(haystack)
	for _, token := range want {
		if !have[token] {
			return false
		}
	}
	return true
}

func tokenSet(s string) map[string]bool {
	tokens := map[string]bool{}
	for _, token := range strings.Fields(textnorm.NormalizeForMatch(s)) {
		tokens[token] = true
	}
	return tokens
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

func (a *SoundCloudAPIAdapter) GetRelatedTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/tracks/%s/related?client_id=%s&limit=%d",
			a.baseURL, url.PathEscape(externalID), url.QueryEscape(clientID), scRelatedLimit,
		)
	}, mapSoundCloudAPITrack)
}

func (a *SoundCloudAPIAdapter) sameHostNextURL(nextHref, clientID string) string {
	if nextHref == "" {
		return ""
	}
	base, err := url.Parse(a.baseURL)
	if err != nil {
		return ""
	}
	next, err := url.Parse(nextHref)
	if err != nil {
		return ""
	}
	if !sameHost(base, next) {
		return ""
	}
	return appendClientID(nextHref, clientID)
}

func sameHost(base, other *url.URL) bool {
	return other.Scheme == base.Scheme && other.Host == base.Host
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
