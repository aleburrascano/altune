package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

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
		return nil, fmt.Errorf("lastfm returned %d", resp.StatusCode)
	}

	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
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
				results = append(results, domain.SearchResult{
					Kind:       domain.ResultKindTrack,
					Title:      t.Name,
					Subtitle:   t.Artist,
					ImageURL:   imageURL,
					Confidence: domain.ConfidenceLow,
					Sources: []domain.SourceRef{{
						Provider:   domain.ProviderLastFM,
						ExternalID: lastfmExternalID(t.URL),
						URL:        t.URL,
					}},
					Extras: extras,
				})
			}
		}
	case domain.ResultKindAlbum:
		var resp struct {
			Results struct {
				AlbumMatches struct {
					Album []struct {
						Name   string `json:"name"`
						Artist string `json:"artist"`
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
				results = append(results, domain.SearchResult{
					Kind:       domain.ResultKindAlbum,
					Title:      a.Name,
					Subtitle:   a.Artist,
					ImageURL:   imageURL,
					Confidence: domain.ConfidenceLow,
					Sources: []domain.SourceRef{{
						Provider:   domain.ProviderLastFM,
						ExternalID: lastfmExternalID(a.URL),
						URL:        a.URL,
					}},
					Extras: make(map[string]any),
				})
			}
		}
	case domain.ResultKindArtist:
		var resp struct {
			Results struct {
				ArtistMatches struct {
					Artist []struct {
						Name      string `json:"name"`
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
				results = append(results, domain.SearchResult{
					Kind:       domain.ResultKindArtist,
					Title:      a.Name,
					ImageURL:   imageURL,
					Confidence: domain.ConfidenceLow,
					Sources: []domain.SourceRef{{
						Provider:   domain.ProviderLastFM,
						ExternalID: lastfmExternalID(a.URL),
						URL:        a.URL,
					}},
					Extras: extras,
				})
			}
		}
	}

	return results
}

// --- ArtistContentProvider ---

func (a *LastFmAdapter) GetArtistTopTracks(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=artist.gettoptracks&artist=%s&api_key=%s&format=json&limit=10",
		url.QueryEscape(artistName), a.apiKey)

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
		return nil, fmt.Errorf("lastfm artist.gettoptracks returned %d", resp.StatusCode)
	}

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
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
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
		results = append(results, domain.SearchResult{
			Kind:       domain.ResultKindTrack,
			Title:      t.Name,
			Subtitle:   t.Artist.Name,
			ImageURL:   imageURL,
			Confidence: domain.ConfidenceLow,
			Sources: []domain.SourceRef{{
				Provider:   domain.ProviderLastFM,
				ExternalID: lastfmExternalID(t.URL),
				URL:        t.URL,
			}},
			Extras: extras,
		})
	}
	return results, nil
}

func (a *LastFmAdapter) GetArtistAlbums(ctx context.Context, _ domain.ProviderName, artistName string) ([]domain.SearchResult, error) {
	u := fmt.Sprintf("https://ws.audioscrobbler.com/2.0/?method=artist.gettopalbums&artist=%s&api_key=%s&format=json&limit=50",
		url.QueryEscape(artistName), a.apiKey)

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
		return nil, fmt.Errorf("lastfm artist.gettopalbums returned %d", resp.StatusCode)
	}

	var body struct {
		TopAlbums struct {
			Album []struct {
				Name      string `json:"name"`
				PlayCount int    `json:"playcount"`
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
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}

	results := make([]domain.SearchResult, 0, len(body.TopAlbums.Album))
	for _, a := range body.TopAlbums.Album {
		if a.Name == "(null)" || a.Name == "" {
			continue
		}
		imageURL := ""
		for _, img := range a.Image {
			if img.Size == "extralarge" {
				imageURL = img.Text
			}
		}
		results = append(results, domain.SearchResult{
			Kind:       domain.ResultKindAlbum,
			Title:      a.Name,
			Subtitle:   a.Artist.Name,
			ImageURL:   imageURL,
			Confidence: domain.ConfidenceLow,
			Sources: []domain.SourceRef{{
				Provider:   domain.ProviderLastFM,
				ExternalID: lastfmExternalID(a.URL),
				URL:        a.URL,
			}},
			Extras: make(map[string]any),
		})
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
		return nil, fmt.Errorf("lastfm chart returned %d", resp.StatusCode)
	}
	var raw json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
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
