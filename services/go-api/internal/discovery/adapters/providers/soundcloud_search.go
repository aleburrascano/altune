package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

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

func (a *SoundCloudAPIAdapter) GetRelatedTracks(ctx context.Context, _ domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	return scFetchList(ctx, a, func(clientID string) string {
		return fmt.Sprintf(
			"%s/tracks/%s/related?client_id=%s&limit=%d",
			a.baseURL, url.PathEscape(externalID), url.QueryEscape(clientID), scRelatedLimit,
		)
	}, mapSoundCloudAPITrack)
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
