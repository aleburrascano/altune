package deezer

import (
	"altune/go-api/internal/search/domain"
	"context"
	"fmt"
)

func (a *DeezerAdapter) FetchCharts(ctx context.Context, limit int) ([]domain.SpellingHintsEntry, error) {
	var entries []domain.SpellingHintsEntry
	for _, kind := range []string{"tracks", "artists", "albums"} {
		items, err := a.fetchChartKind(ctx, kind, limit)
		if err != nil {
			continue
		}
		entries = append(entries, items...)
	}
	return entries, nil
}

func (a *DeezerAdapter) fetchChartKind(
	ctx context.Context,
	kind string,
	limit int,
) ([]domain.SpellingHintsEntry, error) {
	u := fmt.Sprintf(
		deezerAPIBaseURL+"/chart/0/%s?limit=%d",
		kind, limit,
	)
	return a.fetchChartEntries(ctx, u, kind)
}

func (a *DeezerAdapter) fetchChartEntries(
	ctx context.Context,
	u string,
	kind string,
) ([]domain.SpellingHintsEntry, error) {
	var body deezerSearchResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	return mapDeezerChartItems(body.Data, kind), nil
}

func mapDeezerChartItems(
	items []deezerItem,
	kind string,
) []domain.SpellingHintsEntry {
	entries := make([]domain.SpellingHintsEntry, 0, len(items))
	for i, item := range items {
		e := deezerChartEntry(item, kind, i)
		if e.Term == "" {
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

func deezerChartEntry(
	item deezerItem,
	kind string,
	position int,
) domain.SpellingHintsEntry {
	switch kind {
	case "artists":
		return domain.SpellingHintsEntry{
			Term:       item.Name,
			Kind:       domain.VocabKindArtist,
			Popularity: popularityOrPosition(item.NbFan, position),
		}
	case "albums":
		return domain.SpellingHintsEntry{
			Term:       item.Title,
			Kind:       domain.VocabKindAlbum,
			Popularity: popularityOrPosition(item.NbFan, position),
		}
	default:
		return domain.SpellingHintsEntry{
			Term:       item.Title,
			Kind:       domain.VocabKindTrack,
			Popularity: popularityOrPosition(item.Rank, position),
		}
	}
}

func popularityOrPosition(metric int64, position int) int64 {
	if metric > 0 {
		return metric
	}
	return int64(1000 - position)
}
