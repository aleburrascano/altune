package main

import (
	"altune/go-api/internal/discovery/adapters/providers/applemusic"
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"golang.org/x/time/rate"
)

const (
	deezerBaseURL          = "https://api.deezer.com"
	deezerUserAgent        = "altune-librarybind"
	deezerRequestsPerSec   = 2.0
	deezerQuotaCode        = 4
	deezerNoDataCode       = 800
	musicBrainzBaseURL     = "https://musicbrainz.org"
	musicBrainzUserAgent   = "altune-librarybind/1.0 ( https://github.com/aleburrascano/altune )"
	musicBrainzRequestsSec = 1.0
	musicBrainzResultLimit = 25
	appleRequestsPerSec    = 1.0
	millisPerSecond        = 1000.0
)

type isrcLookup interface {
	Name() source
	ByISRC(ctx context.Context, isrc string) ([]candidate, error)
}

type deezerLookup struct{ client *catalogClient }

func newDeezerLookup(httpClient *http.Client, baseURL string, requestsPerSecond float64) deezerLookup {
	return deezerLookup{client: newCatalogClient(httpClient, baseURL, deezerUserAgent, requestsPerSecond, isDeezerThrottle)}
}

func (deezerLookup) Name() source { return sourceDeezer }

type deezerTrackBody struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	ISRC     string `json:"isrc"`
	Duration int    `json:"duration"`
	Artist   struct {
		Name string `json:"name"`
	} `json:"artist"`
	Contributors []struct {
		Name string `json:"name"`
	} `json:"contributors"`
	Album struct {
		Title string `json:"title"`
	} `json:"album"`
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func deezerErrorCode(body []byte) int {
	var parsed deezerTrackBody
	if json.Unmarshal(body, &parsed) != nil || parsed.Error == nil {
		return 0
	}
	return parsed.Error.Code
}

func isDeezerThrottle(status int, body []byte) bool {
	return status == http.StatusTooManyRequests || deezerErrorCode(body) == deezerQuotaCode
}

func (d deezerLookup) ByISRC(ctx context.Context, isrc string) ([]candidate, error) {
	raw, err := d.client.get(ctx, "/2.0/track/isrc:"+url.PathEscape(canonicalISRC(isrc)))
	if errors.Is(err, errCatalogUnknown) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var body deezerTrackBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, blocked("non-JSON deezer response for %s", isrc)
	}
	if body.Error != nil && body.Error.Code == deezerNoDataCode {
		return nil, nil
	}
	if body.Error != nil {
		return nil, fmt.Errorf("deezer error code %d", body.Error.Code)
	}
	return []candidate{deezerCandidate(body)}, nil
}

func deezerCandidate(body deezerTrackBody) candidate {
	artists := []string{body.Artist.Name}
	for _, contributor := range body.Contributors {
		artists = append(artists, contributor.Name)
	}
	return candidate{
		ID: strconv.FormatInt(body.ID, 10), ISRC: body.ISRC, Title: body.Title, Artists: artists,
		Album: body.Album.Title, LengthSeconds: float64(body.Duration),
	}
}

type musicBrainzLookup struct{ client *catalogClient }

func newMusicBrainzLookup(httpClient *http.Client, baseURL string, requestsPerSecond float64) musicBrainzLookup {
	return musicBrainzLookup{client: newCatalogClient(httpClient, baseURL, musicBrainzUserAgent, requestsPerSecond, isMusicBrainzThrottle)}
}

func (musicBrainzLookup) Name() source { return sourceMusicBrainz }

func isMusicBrainzThrottle(status int, _ []byte) bool {
	return status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests
}

type musicBrainzArtist struct {
	Name    string `json:"name"`
	Aliases []struct {
		Name string `json:"name"`
	} `json:"aliases"`
}

type musicBrainzRecording struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Length       int      `json:"length"`
	ISRCs        []string `json:"isrcs"`
	ArtistCredit []struct {
		Name   string            `json:"name"`
		Artist musicBrainzArtist `json:"artist"`
	} `json:"artist-credit"`
}

type musicBrainzSearch struct {
	Recordings []musicBrainzRecording `json:"recordings"`
}

func (m musicBrainzLookup) ByISRC(ctx context.Context, isrc string) ([]candidate, error) {
	query := `isrc:"` + canonicalISRC(isrc) + `"`
	path := "/ws/2/recording?query=" + url.QueryEscape(query) + "&inc=isrcs&fmt=json&limit=" + strconv.Itoa(musicBrainzResultLimit)
	raw, err := m.client.get(ctx, path)
	if errors.Is(err, errCatalogUnknown) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var body musicBrainzSearch
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("decode musicbrainz response: %w", err)
	}
	found := make([]candidate, 0, len(body.Recordings))
	for _, rec := range body.Recordings {
		found = append(found, musicBrainzCandidate(rec, isrc))
	}
	return found, nil
}

func musicBrainzCandidate(rec musicBrainzRecording, wanted string) candidate {
	var artists []string
	for _, credit := range rec.ArtistCredit {
		artists = append(artists, credit.Name, credit.Artist.Name)
		for _, alias := range credit.Artist.Aliases {
			artists = append(artists, alias.Name)
		}
	}
	isrc := ""
	for _, listed := range rec.ISRCs {
		if canonicalISRC(listed) == canonicalISRC(wanted) {
			isrc = listed
		}
	}
	return candidate{ID: rec.ID, ISRC: isrc, Title: rec.Title, Artists: artists, LengthSeconds: float64(rec.Length) / millisPerSecond}
}

type appleSongs interface {
	SongsByISRC(ctx context.Context, isrc string) ([]domain.SearchResult, error)
}

type appleLookup struct {
	songs   appleSongs
	limiter *rate.Limiter
}

func newAppleLookup(songs appleSongs, requestsPerSecond float64) appleLookup {
	return appleLookup{songs: songs, limiter: rate.NewLimiter(rate.Limit(requestsPerSecond), 1)}
}

func newAppleAdapter(httpClient *http.Client) appleSongs {
	return applemusic.NewAppleMusicAdapter(httpClient)
}

func (appleLookup) Name() source { return sourceApple }

func (a appleLookup) ByISRC(ctx context.Context, isrc string) ([]candidate, error) {
	if err := a.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	songs, err := a.songs.SongsByISRC(ctx, canonicalISRC(isrc))
	var statusErr providerhttp.StatusError
	if errors.As(err, &statusErr) && (statusErr.Status == http.StatusForbidden || statusErr.Status == http.StatusTooManyRequests) {
		return nil, blocked("apple music HTTP %d", statusErr.Status)
	}
	if err != nil {
		return nil, err
	}
	found := make([]candidate, 0, len(songs))
	for _, song := range songs {
		found = append(found, candidate{
			ID: song.Sources[0].ExternalID, ISRC: song.ISRC, Title: song.Title, Artists: []string{song.Subtitle},
			Album: song.Album, LengthSeconds: float64(song.Duration),
		})
	}
	return found, nil
}
