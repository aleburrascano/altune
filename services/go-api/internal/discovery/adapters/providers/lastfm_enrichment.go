package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

var _ ports.LastFmEnricher = (*LastFmAdapter)(nil)

const (
	lastfmTagsCap    = 6
	lastfmSimilarCap = 8
)

func (a *LastFmAdapter) Lookup(
	ctx context.Context,
	kind domain.ResultKind,
	artistName, entityTitle string,
) (domain.LastFmEnrichment, error) {
	switch kind {
	case domain.ResultKindArtist:
		return a.lookupArtistInfo(ctx, artistName)
	case domain.ResultKindTrack:
		return a.lookupTrackInfo(ctx, artistName, entityTitle)
	case domain.ResultKindAlbum:
		return a.lookupAlbumInfo(ctx, artistName, entityTitle)
	default:
		return domain.EmptyLastFmEnrichment(), nil
	}
}

const lastfmErrNotFound = 6

type lastfmAPIError struct {
	Code    int    `json:"error"`
	Message string `json:"message"`
}

func (e *lastfmAPIError) Error() string {
	return fmt.Sprintf("lastfm api error %d: %s", e.Code, e.Message)
}

func (a *LastFmAdapter) getInfo(ctx context.Context, u string) ([]byte, error) {
	status, body, err := getBytes(ctx, a.client, u)
	var envelope lastfmAPIError
	if len(body) > 0 && json.Unmarshal(body, &envelope) == nil && envelope.Code != 0 {
		if envelope.Code == lastfmErrNotFound {
			return nil, nil
		}
		return nil, &envelope
	}
	if err != nil {
		return nil, fmt.Errorf("lastfm getinfo status %d: %w", status, err)
	}
	return body, nil
}

func (a *LastFmAdapter) lookupArtistInfo(ctx context.Context, artistName string) (domain.LastFmEnrichment, error) {
	if strings.TrimSpace(artistName) == "" {
		return domain.EmptyLastFmEnrichment(), nil
	}
	u := fmt.Sprintf(
		"https://ws.audioscrobbler.com/2.0/?method=artist.getinfo&artist=%s&autocorrect=1&api_key=%s&format=json",
		url.QueryEscape(artistName), a.apiKey,
	)
	body, err := a.getInfo(ctx, u)
	if err != nil {
		return domain.EmptyLastFmEnrichment(), err
	}
	if len(body) == 0 {
		return domain.EmptyLastFmEnrichment(), nil
	}
	var resp struct {
		Artist struct {
			MBID  string `json:"mbid"`
			Stats struct {
				Listeners string `json:"listeners"`
				Playcount string `json:"playcount"`
			} `json:"stats"`
			Tags    json.RawMessage `json:"tags"`
			Similar json.RawMessage `json:"similar"`
			Bio     struct {
				Summary string `json:"summary"`
			} `json:"bio"`
		} `json:"artist"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return domain.EmptyLastFmEnrichment(), err
	}

	e := domain.EmptyLastFmEnrichment()
	e.MBID = strings.TrimSpace(resp.Artist.MBID)
	e.Listeners = parseListeners(resp.Artist.Stats.Listeners)
	e.Playcount = parseListeners(resp.Artist.Stats.Playcount)
	e.Tags = parseLastFmTags(resp.Artist.Tags)
	e.Similar = parseLastFmSimilarArtists(resp.Artist.Similar)
	e.Bio = cleanLastFmBio(resp.Artist.Bio.Summary)
	return e, nil
}

func (a *LastFmAdapter) lookupTrackInfo(ctx context.Context, artistName, track string) (domain.LastFmEnrichment, error) {
	if strings.TrimSpace(artistName) == "" || strings.TrimSpace(track) == "" {
		return domain.EmptyLastFmEnrichment(), nil
	}
	u := fmt.Sprintf(
		"https://ws.audioscrobbler.com/2.0/?method=track.getinfo&artist=%s&track=%s&autocorrect=1&api_key=%s&format=json",
		url.QueryEscape(artistName), url.QueryEscape(track), a.apiKey,
	)
	body, err := a.getInfo(ctx, u)
	if err != nil {
		return domain.EmptyLastFmEnrichment(), err
	}
	if len(body) == 0 {
		return domain.EmptyLastFmEnrichment(), nil
	}
	var resp struct {
		Track struct {
			MBID      string          `json:"mbid"`
			Listeners string          `json:"listeners"`
			Playcount string          `json:"playcount"`
			Duration  string          `json:"duration"`
			Album     json.RawMessage `json:"album"`
			TopTags   json.RawMessage `json:"toptags"`
			Wiki      struct {
				Summary string `json:"summary"`
			} `json:"wiki"`
		} `json:"track"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return domain.EmptyLastFmEnrichment(), err
	}

	e := domain.EmptyLastFmEnrichment()
	e.MBID = strings.TrimSpace(resp.Track.MBID)
	e.Listeners = parseListeners(resp.Track.Listeners)
	e.Playcount = parseListeners(resp.Track.Playcount)
	e.Duration = int(parseListeners(resp.Track.Duration) / 1000)
	e.Album = parseLastFmAlbumTitle(resp.Track.Album)
	e.Tags = parseLastFmTags(resp.Track.TopTags)
	e.Bio = cleanLastFmBio(resp.Track.Wiki.Summary)
	return e, nil
}

func (a *LastFmAdapter) lookupAlbumInfo(ctx context.Context, artistName, album string) (domain.LastFmEnrichment, error) {
	if strings.TrimSpace(artistName) == "" || strings.TrimSpace(album) == "" {
		return domain.EmptyLastFmEnrichment(), nil
	}
	u := fmt.Sprintf(
		"https://ws.audioscrobbler.com/2.0/?method=album.getinfo&artist=%s&album=%s&autocorrect=1&api_key=%s&format=json",
		url.QueryEscape(artistName), url.QueryEscape(album), a.apiKey,
	)
	body, err := a.getInfo(ctx, u)
	if err != nil {
		return domain.EmptyLastFmEnrichment(), err
	}
	if len(body) == 0 {
		return domain.EmptyLastFmEnrichment(), nil
	}
	var resp struct {
		Album struct {
			MBID      string          `json:"mbid"`
			Listeners string          `json:"listeners"`
			Playcount string          `json:"playcount"`
			Tags      json.RawMessage `json:"tags"`
			Wiki      struct {
				Summary string `json:"summary"`
			} `json:"wiki"`
		} `json:"album"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return domain.EmptyLastFmEnrichment(), err
	}

	e := domain.EmptyLastFmEnrichment()
	e.MBID = strings.TrimSpace(resp.Album.MBID)
	e.Listeners = parseListeners(resp.Album.Listeners)
	e.Playcount = parseListeners(resp.Album.Playcount)
	e.Tags = parseLastFmTags(resp.Album.Tags)
	e.Bio = cleanLastFmBio(resp.Album.Wiki.Summary)
	return e, nil
}

func parseLastFmTags(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var parsed struct {
		Tag []struct {
			Name string `json:"name"`
		} `json:"tag"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return []string{}
	}
	out := make([]string, 0, len(parsed.Tag))
	seen := make(map[string]bool, len(parsed.Tag))
	for _, t := range parsed.Tag {
		name := strings.TrimSpace(t.Name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
		if len(out) >= lastfmTagsCap {
			break
		}
	}
	return out
}

func parseLastFmSimilarArtists(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var parsed struct {
		Artist []struct {
			Name string `json:"name"`
		} `json:"artist"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return []string{}
	}
	out := make([]string, 0, len(parsed.Artist))
	for _, art := range parsed.Artist {
		name := strings.TrimSpace(art.Name)
		if name == "" {
			continue
		}
		out = append(out, name)
		if len(out) >= lastfmSimilarCap {
			break
		}
	}
	return out
}

func parseLastFmAlbumTitle(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var parsed struct {
		Title string `json:"title"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Title)
}

var lastfmReadMore = regexp.MustCompile(`(?s)\s*<a[^>]*>.*$`)

var lastfmHTMLTag = regexp.MustCompile(`<[^>]+>`)

func cleanLastFmBio(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	out := lastfmReadMore.ReplaceAllString(summary, "")
	out = lastfmHTMLTag.ReplaceAllString(out, "")
	out = html.UnescapeString(out)
	return strings.TrimSpace(out)
}
