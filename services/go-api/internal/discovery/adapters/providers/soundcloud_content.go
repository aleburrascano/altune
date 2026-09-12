package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"altune/go-api/internal/discovery/domain"
)

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
