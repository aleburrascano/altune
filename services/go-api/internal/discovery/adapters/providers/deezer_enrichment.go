package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// AIDEV-NOTE: Deezer detail-open enrichment (docs/providers/deezer.md caps 7–8).
// Resolve a (kind, artist, title) to a Deezer id via search, then one /track/{id}
// or /album/{id} lookup yields the audio fields (bpm/gain), explicit flag, and
// album liner data (label/genres/upc/record_type) the thin search projection
// drops. Response shapes live-probed 2026-06-22 (docs/providers/deezer.md §4).
// Off the ranking path — display-only. Lyrics (cap 6) are a separate feature.

var _ ports.DeezerEnricher = (*DeezerAdapter)(nil)

const deezerGenresCap = 6

// ResolveID maps a (kind, artist, title) to the top-matching Deezer id via the
// structured search, returning "" when nothing matches. Mirrors the Discogs
// ResolveMasterID step so the service can negatively-cache an unresolved name.
func (a *DeezerAdapter) ResolveID(
	ctx context.Context,
	kind domain.ResultKind,
	artist, title string,
) (string, error) {
	if kind != domain.ResultKindTrack && kind != domain.ResultKindAlbum {
		return "", nil
	}
	if strings.TrimSpace(title) == "" {
		return "", nil
	}
	results, err := a.searchKind(ctx, deezerStructuredQuery(artist, title, kind), kind)
	if err != nil {
		return "", err
	}
	for _, r := range results {
		if len(r.Sources) > 0 && r.Sources[0].ExternalID != "" {
			return r.Sources[0].ExternalID, nil
		}
	}
	return "", nil
}

// Lookup fetches the detail for a known Deezer id and assembles the enrichment.
// A non-200 or decode failure returns an error so the service can degrade to
// empty.
func (a *DeezerAdapter) Lookup(
	ctx context.Context,
	kind domain.ResultKind,
	id string,
) (domain.DeezerEnrichment, error) {
	switch kind {
	case domain.ResultKindTrack:
		return a.lookupTrackDetail(ctx, id)
	case domain.ResultKindAlbum:
		return a.lookupAlbumDetail(ctx, id)
	default:
		return domain.EmptyDeezerEnrichment(), nil
	}
}

func (a *DeezerAdapter) lookupTrackDetail(ctx context.Context, id string) (domain.DeezerEnrichment, error) {
	var detail struct {
		BPM          float64             `json:"bpm"`
		Gain         float64             `json:"gain"`
		Explicit     bool                `json:"explicit_lyrics"`
		Contributors []deezerContributor `json:"contributors"`
	}
	u := fmt.Sprintf("https://api.deezer.com/track/%s", url.PathEscape(id))
	if err := a.getJSON(ctx, u, &detail); err != nil {
		return domain.EmptyDeezerEnrichment(), err
	}
	e := domain.EmptyDeezerEnrichment()
	e.BPM = int(math.Round(detail.BPM))
	e.Gain = detail.Gain
	e.Explicit = detail.Explicit
	e.Featured = extractDeezerFeatured(detail.Contributors)
	return e, nil
}

func (a *DeezerAdapter) lookupAlbumDetail(ctx context.Context, id string) (domain.DeezerEnrichment, error) {
	var detail struct {
		UPC        string `json:"upc"`
		Label      string `json:"label"`
		RecordType string `json:"record_type"`
		Genres     struct {
			Data []struct {
				Name string `json:"name"`
			} `json:"data"`
		} `json:"genres"`
		Contributors []deezerContributor `json:"contributors"`
	}
	u := fmt.Sprintf("https://api.deezer.com/album/%s", url.PathEscape(id))
	if err := a.getJSON(ctx, u, &detail); err != nil {
		return domain.EmptyDeezerEnrichment(), err
	}
	e := domain.EmptyDeezerEnrichment()
	e.UPC = strings.TrimSpace(detail.UPC)
	e.Label = strings.TrimSpace(detail.Label)
	e.RecordType = strings.TrimSpace(detail.RecordType)
	e.Genres = dedupeDeezerGenres(detail.Genres.Data)
	// Collab albums (e.g. "Her Loss" — Drake & 21 Savage) list co-primary artists
	// as contributors; surface the non-primary ones for the album artist line.
	e.Featured = extractDeezerFeatured(detail.Contributors)
	return e, nil
}

// getJSON performs a GET and decodes the body into dst; a non-200 is an error,
// and so is Deezer's in-band {"error":{...}} envelope (quota exhaustion and
// invalid ids ride on HTTP 200 — see deezerAPIError). Every public-API call in
// deezer.go and this file routes through here.
func (a *DeezerAdapter) getJSON(ctx context.Context, u string, dst any) error {
	var raw json.RawMessage
	if err := getJSON(ctx, a.client, u, &raw); err != nil {
		return err
	}
	var envelope struct {
		Error *deezerAPIError `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error != nil {
		return envelope.Error
	}
	return json.Unmarshal(raw, dst)
}

// dedupeDeezerGenres pulls genre names from the `{data:[{name}]}` shape, trimmed,
// case-insensitively deduped, and capped.
func dedupeDeezerGenres(data []struct {
	Name string `json:"name"`
}) []string {
	out := make([]string, 0, len(data))
	seen := make(map[string]bool, len(data))
	for _, g := range data {
		name := strings.TrimSpace(g.Name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		out = append(out, name)
		if len(out) >= deezerGenresCap {
			break
		}
	}
	return out
}
