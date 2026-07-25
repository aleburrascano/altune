package providers

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

var _ ports.MetadataEnricher = (*MusicBrainzAdapter)(nil)

func (a *MusicBrainzAdapter) Lookup(ctx context.Context, kind domain.ResultKind, mbid string) (domain.MBEnrichment, error) {
	if mbid == "" {
		return domain.EmptyEnrichment(), nil
	}

	switch kind {
	case domain.ResultKindArtist:
		u := fmt.Sprintf(
			"https://musicbrainz.org/ws/2/artist/%s?inc=genres+ratings+url-rels&fmt=json",
			url.PathEscape(mbid),
		)
		var body mbLookupArtist
		if err := a.getJSON(ctx, u, &body); err != nil {
			return domain.EmptyEnrichment(), err
		}
		e := domain.EmptyEnrichment()
		e.MBID = mbid
		e.Genres = sortedGenres(body.Genres)
		e.Rating = body.Rating.Value
		e.RatingVotes = body.Rating.VotesCount
		e.ExternalIDs = externalIDsFromRelations(body.Relations)
		return e, nil

	case domain.ResultKindAlbum:
		u := fmt.Sprintf(
			"https://musicbrainz.org/ws/2/release-group/%s?inc=genres+ratings&fmt=json",
			url.PathEscape(mbid),
		)
		var body mbLookupReleaseGroup
		if err := a.getJSON(ctx, u, &body); err != nil {
			return domain.EmptyEnrichment(), err
		}
		e := domain.EmptyEnrichment()
		e.MBID = mbid
		e.Genres = sortedGenres(body.Genres)
		e.Rating = body.Rating.Value
		e.RatingVotes = body.Rating.VotesCount
		e.Year = parseBirthYear(body.FirstReleaseDate)
		e.PrimaryType = body.PrimaryType
		if len(body.SecondaryTypes) > 0 {
			e.SecondaryTypes = body.SecondaryTypes
		}
		return e, nil

	default:
		return domain.EmptyEnrichment(), nil
	}
}

func (a *MusicBrainzAdapter) ResolveMBID(ctx context.Context, kind domain.ResultKind, title, subtitle string) (string, error) {
	titleNorm := textnorm.NormalizeForMatch(title)
	if titleNorm == "" {
		return "", nil
	}
	subtitleNorm := textnorm.NormalizeForMatch(subtitle)

	switch kind {
	case domain.ResultKindArtist:
		artists, err := a.fetchArtistMatches(ctx, title)
		if err != nil {
			return "", err
		}
		for _, art := range artists {
			if textnorm.NormalizeForMatch(art.Name) == titleNorm {
				return art.ID, nil
			}
		}
		return "", nil

	case domain.ResultKindAlbum:
		groups, err := a.fetchReleaseGroupMatches(ctx, title)
		if err != nil {
			return "", err
		}
		for _, rg := range groups {
			if textnorm.NormalizeForMatch(rg.Title) != titleNorm {
				continue
			}
			if subtitleNorm != "" && !creditMatches(rg.ArtistCredit, subtitleNorm) {
				continue
			}
			return rg.ID, nil
		}
		return "", nil

	case domain.ResultKindTrack:
		recs, err := a.fetchRecordingMatches(ctx, title)
		if err != nil {
			return "", err
		}
		for _, rec := range recs {
			if textnorm.NormalizeForMatch(rec.Title) != titleNorm {
				continue
			}
			if subtitleNorm != "" && !creditMatches(rec.ArtistCredit, subtitleNorm) {
				continue
			}
			return rec.ID, nil
		}
		return "", nil

	default:
		return "", nil
	}
}

func creditMatches(credit []mbArtistRef, wantNorm string) bool {
	if len(credit) == 0 {
		return false
	}
	return textnorm.NormalizeForMatch(credit[0].Name) == wantNorm
}

func sortedGenres(genres []mbGenre) []string {
	if len(genres) == 0 {
		return []string{}
	}
	type g struct {
		name  string
		count int
	}
	seen := make(map[string]int, len(genres))
	deduped := make([]g, 0, len(genres))
	for _, raw := range genres {
		name := strings.TrimSpace(raw.Name)
		if name == "" {
			continue
		}
		if idx, ok := seen[name]; ok {
			if raw.Count > deduped[idx].count {
				deduped[idx].count = raw.Count
			}
			continue
		}
		seen[name] = len(deduped)
		deduped = append(deduped, g{name: name, count: raw.Count})
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		if deduped[i].count != deduped[j].count {
			return deduped[i].count > deduped[j].count
		}
		return deduped[i].name < deduped[j].name
	})
	out := make([]string, len(deduped))
	for i := range deduped {
		out[i] = deduped[i].name
	}
	return out
}

func externalIDsFromRelations(relations []mbRelation) map[string]string {
	ids := map[string]string{}
	put := func(key, raw string) {
		if key == "" {
			return
		}
		if _, exists := ids[key]; exists {
			return
		}
		if id := lastPathSegment(raw); id != "" {
			ids[key] = id
		}
	}
	for _, rel := range relations {
		res := rel.URL.Resource
		switch rel.Type {
		case "discogs":
			put("discogs", res)
		case "wikidata":
			put("wikidata", res)
		case "soundcloud":
			put("soundcloud", res)
		case "free streaming", "streaming", "purchase for download":
			switch {
			case strings.Contains(res, "open.spotify.com"):
				put("spotify", res)
			case strings.Contains(res, "deezer.com"):
				put("deezer", res)
			case strings.Contains(res, "music.apple.com"):
				put("itunes", res)
			}
		}
	}
	return ids
}

func lastPathSegment(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	p := strings.TrimRight(u.Path, "/")
	if p == "" {
		return ""
	}
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

type mbGenre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type mbRating struct {
	Value      float64 `json:"value"`
	VotesCount int     `json:"votes-count"`
}

type mbRelationURL struct {
	Resource string `json:"resource"`
}

type mbRelation struct {
	Type string        `json:"type"`
	URL  mbRelationURL `json:"url"`
}

type mbLookupArtist struct {
	Genres    []mbGenre    `json:"genres"`
	Rating    mbRating     `json:"rating"`
	Relations []mbRelation `json:"relations"`
}

type mbLookupReleaseGroup struct {
	Genres           []mbGenre `json:"genres"`
	Rating           mbRating  `json:"rating"`
	FirstReleaseDate string    `json:"first-release-date"`
	PrimaryType      string    `json:"primary-type"`
	SecondaryTypes   []string  `json:"secondary-types"`
}
