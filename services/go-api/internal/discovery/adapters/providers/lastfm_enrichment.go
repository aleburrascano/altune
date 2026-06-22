package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// AIDEV-NOTE: Last.fm detail-open enrichment (docs/providers/lastfm.md cap 3,
// with cap-4 similar artists folded into the artist payload). One *.getInfo
// lookup per opened entity yields the listen-based popularity, weighted tags,
// bio, similar-artist graph, and the MBID bridge. Response shapes live-probed
// 2026-06-22 (docs/providers/lastfm.md §4). Off the ranking path — display-only.
//
// Last.fm's getInfo fuzzy-matches names server-side (we pass autocorrect=1), so
// there is no separate id-resolution step like Discogs needs — a single call.

var _ ports.LastFmEnricher = (*LastFmAdapter)(nil)

const (
	lastfmTagsCap    = 6
	lastfmSimilarCap = 8
)

// Lookup dispatches to the per-kind getInfo call. artistName is the artist;
// entityTitle is the track/album title (empty for the artist kind). A non-200
// or decode failure returns an error so the service can degrade to empty.
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

func (a *LastFmAdapter) getInfo(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("lastfm getinfo returned %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
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
	e.Duration = int(parseListeners(resp.Track.Duration) / 1000) // Last.fm reports ms
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

// parseLastFmTags extracts tag names from the `{ "tag": [{name}] }` shape,
// trimmed/deduped/capped, preserving Last.fm's relevance order. Tolerant of the
// empty-collection-as-"" quirk: any parse failure yields no tags.
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

// parseLastFmSimilarArtists extracts similar-artist names from the
// `{ "artist": [{name}] }` shape (cap 4). Same tolerance as tags.
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

// parseLastFmAlbumTitle pulls the album title from a track's `album` object,
// tolerating the field being absent or a non-object.
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

// lastfmReadMore matches the trailing "Read more on Last.fm" anchor every bio
// summary ends with; we cut from the first anchor onward.
var lastfmReadMore = regexp.MustCompile(`(?s)\s*<a[^>]*>.*$`)

// lastfmHTMLTag matches any remaining HTML tag in a bio summary.
var lastfmHTMLTag = regexp.MustCompile(`<[^>]+>`)

// cleanLastFmBio strips the trailing "Read more on Last.fm" link, removes any
// other HTML tags, unescapes entities, and trims. Returns "" for an empty or
// placeholder summary.
func cleanLastFmBio(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return ""
	}
	out := lastfmReadMore.ReplaceAllString(summary, "")
	out = lastfmHTMLTag.ReplaceAllString(out, "")
	out = html.UnescapeString(out)
	return strings.TrimSpace(out)
}
