package musicbrainz

import (
	"altune/go-api/internal/discovery/internal/adapters/providers/providersearch"
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
)

func (a *MusicBrainzAdapter) ResolveArtistIdentity(ctx context.Context, name string) (*ports.ArtistIdentity, error) {
	nameNorm := textnorm.NormalizeForMatch(name)
	if id, ok := a.identityMemo.Get(nameNorm); ok {
		return id, nil
	}

	artists, err := a.fetchArtistMatches(ctx, name)
	if err != nil {
		return nil, err
	}

	var first *mbArtistItem
	candidateCount := 0
	for i := range artists {
		if textnorm.NormalizeForMatch(artists[i].Name) == nameNorm {
			candidateCount++
			if first == nil {
				first = &artists[i]
			}
		}
	}
	if first == nil {
		return nil, ports.ErrArtistIdentityNotFound
	}
	if candidateCount > 1 {
		slog.InfoContext(ctx, "mb.multiple_name_matches",
			"name", name, "candidates", candidateCount,
			"picked_mbid", first.ID,
			"picked_disambiguation", first.Disambiguation,
		)
	}
	birthYear := parseBirthYear(first.LifeSpan.Begin)
	area := ""
	if first.Area != nil {
		area = first.Area.Name
	}
	identity := &ports.ArtistIdentity{
		MBID:           first.ID,
		Disambiguation: first.Disambiguation,
		BirthYear:      birthYear,
		Area:           area,
		ArtistType:     first.Type,
	}
	a.identityMemo.Put(nameNorm, identity)
	return identity, nil
}

func parseBirthYear(begin string) int {
	if len(begin) < 4 {
		return 0
	}
	year := 0
	for _, c := range begin[:4] {
		if c < '0' || c > '9' {
			return 0
		}
		year = year*10 + int(c-'0')
	}
	return year
}

func (a *MusicBrainzAdapter) resolveArtistMBID(ctx context.Context, name string) (string, error) {
	id, err := a.ResolveArtistIdentity(ctx, name)
	if errors.Is(err, ports.ErrArtistIdentityNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return id.MBID, nil
}

func (a *MusicBrainzAdapter) fetchArtistMatches(ctx context.Context, name string) ([]mbArtistItem, error) {
	u := fmt.Sprintf(musicbrainzAPIBaseURL+"/artist/?query=%s&fmt=json&limit=5",
		url.QueryEscape(providersearch.MbLuceneEscape(name)))

	var body mbArtistResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}
	return body.Artists, nil
}
