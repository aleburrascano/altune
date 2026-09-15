package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"encoding/json"
	"fmt"
)

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
