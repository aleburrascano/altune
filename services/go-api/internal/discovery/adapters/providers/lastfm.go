package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
)

type LastFmAdapter struct {
	client *http.Client
	apiKey string
}

func NewLastFmAdapter(client *http.Client, apiKey string) *LastFmAdapter {
	return &LastFmAdapter{client: client, apiKey: apiKey}
}

func (a *LastFmAdapter) Name() domain.ProviderName { return domain.ProviderLastFM }

// SearchTimeout gives Last.fm a budget in line with the other multi-kind
// providers (iTunes 4s, MB 5s). The 1.5s default was the lone omission and, even
// with the per-kind calls now concurrent, Last.fm's endpoints are routinely
// slower than 1.5s — so the default surfaced as spurious timeouts.
func (a *LastFmAdapter) SearchTimeout() time.Duration { return 4 * time.Second }

func (a *LastFmAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *LastFmAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return searchAcrossKinds(ctx, "lastfm", query, kinds, a.SupportedKinds(),
		func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			return a.searchKind(ctx, query, kind)
		})
}

func (a *LastFmAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	method := lastfmMethod(kind)
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=%s&%s=%s&api_key=%s&format=json&limit=15",
		method, lastfmQueryParam(kind), url.QueryEscape(query), a.apiKey)

	var raw json.RawMessage
	if err := getJSON(ctx, a.client, u, &raw); err != nil {
		return nil, err
	}
	return parseLastFmResponse(raw, kind), nil
}

func lastfmMethod(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "track.search"
	case domain.ResultKindAlbum:
		return "album.search"
	case domain.ResultKindArtist:
		return "artist.search"
	default:
		return "track.search"
	}
}

func lastfmQueryParam(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "track"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindArtist:
		return "artist"
	default:
		return "track"
	}
}

func parseLastFmResponse(raw json.RawMessage, kind domain.ResultKind) []domain.SearchResult {
	var results []domain.SearchResult

	switch kind {
	case domain.ResultKindTrack:
		var resp struct {
			Results struct {
				TrackMatches struct {
					Track []struct {
						Name      string `json:"name"`
						Artist    string `json:"artist"`
						MBID      string `json:"mbid"`
						URL       string `json:"url"`
						Listeners string `json:"listeners"`
						Image     []struct {
							Text string `json:"#text"`
							Size string `json:"size"`
						} `json:"image"`
					} `json:"track"`
				} `json:"trackmatches"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &resp) == nil {
			for _, t := range resp.Results.TrackMatches.Track {
				imageURL := ""
				for _, img := range t.Image {
					if img.Size == "extralarge" {
						imageURL = img.Text
					}
				}
				extras := make(map[string]any)
				if t.Listeners != "" {
					extras["listeners"] = t.Listeners
				}
				r := domain.NewProviderResult(domain.ResultKindTrack, t.Name, t.Artist, imageURL,
					domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(t.URL), URL: t.URL},
					extras)
				// mbid lifts the track into the identifier merge tier (same reason
				// GetArtistAlbums keeps it — previously dropped on the floor).
				r.MBID = t.MBID
				results = append(results, r)
			}
		}
	case domain.ResultKindAlbum:
		var resp struct {
			Results struct {
				AlbumMatches struct {
					Album []struct {
						Name   string `json:"name"`
						Artist string `json:"artist"`
						MBID   string `json:"mbid"`
						URL    string `json:"url"`
						Image  []struct {
							Text string `json:"#text"`
							Size string `json:"size"`
						} `json:"image"`
					} `json:"album"`
				} `json:"albummatches"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &resp) == nil {
			for _, a := range resp.Results.AlbumMatches.Album {
				imageURL := ""
				for _, img := range a.Image {
					if img.Size == "extralarge" {
						imageURL = img.Text
					}
				}
				r := domain.NewProviderResult(domain.ResultKindAlbum, a.Name, a.Artist, imageURL,
					domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(a.URL), URL: a.URL},
					nil)
				// AIDEV-NOTE: a.MBID is deliberately NOT mapped onto r.MBID. Last.fm's
				// album-search mbid is a RELEASE MBID, while MusicBrainz album results
				// carry RELEASE-GROUP MBIDs — different UUID namespaces, so stamping
				// it makes sameEntity's MBID hard-stop systematically block every
				// MB↔Last.fm album merge (duplicate album rows). Track (recording
				// namespace, matches MB recordings) and artist mbids stay mapped.
				results = append(results, r)
			}
		}
	case domain.ResultKindArtist:
		var resp struct {
			Results struct {
				ArtistMatches struct {
					Artist []struct {
						Name      string `json:"name"`
						MBID      string `json:"mbid"`
						URL       string `json:"url"`
						Listeners string `json:"listeners"`
						Image     []struct {
							Text string `json:"#text"`
							Size string `json:"size"`
						} `json:"image"`
					} `json:"artist"`
				} `json:"artistmatches"`
			} `json:"results"`
		}
		if json.Unmarshal(raw, &resp) == nil {
			for _, a := range resp.Results.ArtistMatches.Artist {
				imageURL := ""
				for _, img := range a.Image {
					if img.Size == "extralarge" {
						imageURL = img.Text
					}
				}
				extras := make(map[string]any)
				if a.Listeners != "" {
					extras["listeners"] = a.Listeners
				}
				r := domain.NewProviderResult(domain.ResultKindArtist, a.Name, "", imageURL,
					domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(a.URL), URL: a.URL},
					extras)
				// AIDEV-WARNING: a stale Last.fm artist mbid that differs from
				// MusicBrainz's current one both hard-stops sameEntity's MBID tier
				// and can inflate ambiguousArtistNames (≥2 distinct MBIDs per name).
				// Merge-affecting — validated via `discoveryeval -mode merge` A/B.
				r.MBID = a.MBID
				results = append(results, r)
			}
		}
	}

	return results
}

// --- ArtistContentProvider ---

// looksLikeMBID reports whether s is a MusicBrainz UUID (8-4-4-4-12 hex with
// dashes), so the caller can pass an MBID for identity-safe lookups.
func looksLikeMBID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}

func (a *LastFmAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, artistRef string) ([]domain.SearchResult, error) {
	// artistRef is an MBID when the caller has the artist's resolved identity
	// (identity-safe — avoids the ambiguous-name top-tracks problem); otherwise it
	// is the artist name.
	idParam := "artist=" + url.QueryEscape(artistRef)
	if looksLikeMBID(artistRef) {
		idParam = "mbid=" + artistRef
	}
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=artist.gettoptracks&%s&api_key=%s&format=json&limit=10&autocorrect=1",
		idParam, a.apiKey)

	var body struct {
		TopTracks struct {
			Track []struct {
				Name      string `json:"name"`
				PlayCount string `json:"playcount"`
				Listeners string `json:"listeners"`
				URL       string `json:"url"`
				Artist    struct {
					Name string `json:"name"`
				} `json:"artist"`
				Image []struct {
					Text string `json:"#text"`
					Size string `json:"size"`
				} `json:"image"`
			} `json:"track"`
		} `json:"toptracks"`
	}
	if err := getJSON(ctx, a.client, u, &body); err != nil {
		return nil, err
	}

	results := make([]domain.SearchResult, 0, len(body.TopTracks.Track))
	for _, t := range body.TopTracks.Track {
		imageURL := ""
		for _, img := range t.Image {
			if img.Size == "extralarge" {
				imageURL = img.Text
			}
		}
		extras := make(map[string]any)
		if t.PlayCount != "" {
			extras["playcount"] = parseListeners(t.PlayCount)
		}
		if t.Listeners != "" {
			extras["listeners"] = parseListeners(t.Listeners)
		}
		results = append(results, domain.NewProviderResult(domain.ResultKindTrack, t.Name, t.Artist.Name, imageURL,
			domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(t.URL), URL: t.URL},
			extras))
	}
	return results, nil
}

// lastfmAlbumsLimit caps artist.gettopalbums. The default 50 is deliberate, not
// a truncation bug: past the top ~50-by-playcount the method returns the
// artist's entire credited-on graph (compilations, "various artists"
// appearances, live bootlegs, singles-as-albums), not real discography. A prod
// coverage scan at limit=500 exploded the album union 21× with this noise, so
// real deep discography is sourced from MusicBrainz/Deezer (identifier-backed)
// instead. See docs/providers/maximization-audit-2026-06-22.md §3.3.
const lastfmAlbumsLimit = 50

func (a *LastFmAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=artist.gettopalbums&artist=%s&api_key=%s&format=json&limit=%d",
		url.QueryEscape(artistName), a.apiKey, lastfmAlbumsLimit)

	var body struct {
		TopAlbums struct {
			Album []struct {
				Name      string `json:"name"`
				PlayCount int    `json:"playcount"`
				MBID      string `json:"mbid"`
				URL       string `json:"url"`
				Artist    struct {
					Name string `json:"name"`
				} `json:"artist"`
				Image []struct {
					Text string `json:"#text"`
					Size string `json:"size"`
				} `json:"image"`
			} `json:"album"`
		} `json:"topalbums"`
	}
	if err := getJSON(ctx, a.client, u, &body); err != nil {
		return nil, err
	}

	results := make([]domain.SearchResult, 0, len(body.TopAlbums.Album))
	for _, al := range body.TopAlbums.Album {
		if al.Name == "(null)" || al.Name == "" {
			continue
		}
		imageURL := ""
		for _, img := range al.Image {
			if img.Size == "extralarge" {
				imageURL = img.Text
			}
		}
		// mbid bridges the album into identifier-based merge; playcount is a
		// popularity signal — both were previously dropped on the floor.
		extras := make(map[string]any)
		if al.PlayCount > 0 {
			extras["playcount"] = int64(al.PlayCount)
		}
		r := domain.NewProviderResult(domain.ResultKindAlbum, al.Name, al.Artist.Name, imageURL,
			domain.SourceRef{Provider: domain.ProviderLastFM, ExternalID: lastfmExternalID(al.URL), URL: al.URL},
			extras)
		r.MBID = al.MBID
		results = append(results, r)
	}
	return results, nil
}

// --- ChartProvider ---

func (a *LastFmAdapter) FetchCharts(ctx context.Context, limit int) ([]domain.VocabularyEntry, error) {
	var entries []domain.VocabularyEntry
	for _, method := range []string{"chart.gettopartists", "chart.gettoptracks"} {
		items, err := a.fetchChart(ctx, method, limit)
		if err != nil {
			continue
		}
		entries = append(entries, items...)
	}
	return entries, nil
}

func (a *LastFmAdapter) fetchChart(
	ctx context.Context,
	method string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	u := fmt.Sprintf(
		"https://ws.audioscrobbler.com/2.0/?method=%s&limit=%d&api_key=%s&format=json",
		method, limit, a.apiKey,
	)
	var raw json.RawMessage
	if err := getJSON(ctx, a.client, u, &raw); err != nil {
		return nil, err
	}
	return parseLastFmChartResponse(raw, method), nil
}

func parseLastFmChartResponse(
	raw json.RawMessage,
	method string,
) []domain.VocabularyEntry {
	switch method {
	case "chart.gettopartists":
		return parseLastFmChartArtists(raw)
	case "chart.gettoptracks":
		return parseLastFmChartTracks(raw)
	default:
		return nil
	}
}

func parseLastFmChartArtists(raw json.RawMessage) []domain.VocabularyEntry {
	var resp struct {
		Artists struct {
			Artist []lastfmChartArtist `json:"artist"`
		} `json:"artists"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return nil
	}
	entries := make([]domain.VocabularyEntry, 0, len(resp.Artists.Artist))
	for _, a := range resp.Artists.Artist {
		entries = append(entries, domain.VocabularyEntry{
			Term:       a.Name,
			Kind:       "artist",
			Popularity: parseListeners(a.Listeners),
		})
	}
	return entries
}

func parseLastFmChartTracks(raw json.RawMessage) []domain.VocabularyEntry {
	var resp struct {
		Tracks struct {
			Track []lastfmChartTrack `json:"track"`
		} `json:"tracks"`
	}
	if json.Unmarshal(raw, &resp) != nil {
		return nil
	}
	entries := make([]domain.VocabularyEntry, 0, len(resp.Tracks.Track))
	for _, t := range resp.Tracks.Track {
		entries = append(entries, domain.VocabularyEntry{
			Term:       t.Name,
			Kind:       "track",
			Popularity: parseListeners(t.Listeners),
		})
	}
	return entries
}

type lastfmChartArtist struct {
	Name      string `json:"name"`
	Listeners string `json:"listeners"`
}

type lastfmChartTrack struct {
	Name      string `json:"name"`
	Listeners string `json:"listeners"`
}

func parseListeners(s string) int64 {
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		}
	}
	return n
}

// lastfmExternalID derives an external ID from a Last.fm URL.
// e.g. "https://www.last.fm/music/The+Weeknd" → "The+Weeknd"
// e.g. "https://www.last.fm/music/Katy+Perry/_/Small+Talk" → "Katy+Perry/_/Small+Talk"
func lastfmExternalID(u string) string {
	const prefix = "/music/"
	idx := strings.Index(u, prefix)
	if idx < 0 {
		if u != "" {
			return u
		}
		return ""
	}
	id := u[idx+len(prefix):]
	id = strings.TrimRight(id, "/")
	if id == "" {
		return u
	}
	return id
}
