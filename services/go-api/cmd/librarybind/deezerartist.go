package main

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	deezerArtistSearchLimit = 5
	deezerAlbumLimit        = 100
	deezerTopTrackLimit     = 50
	deezerSongSearchLimit   = 25
	maxExpandedSongs        = 5
)

type deezerArtistSource struct{ client *catalogClient }

func (deezerArtistSource) Name() source { return sourceDeezer }

type deezerListBody[T any] struct {
	Data  []T `json:"data"`
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

type deezerArtistHit struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type deezerTitleHit struct {
	Title string `json:"title"`
}

type deezerSongHit struct {
	ID       int64           `json:"id"`
	Title    string          `json:"title"`
	Duration int             `json:"duration"`
	Artist   deezerArtistHit `json:"artist"`
	Album    deezerTitleHit  `json:"album"`
}

func deezerList[T any](ctx context.Context, client *catalogClient, path string) ([]T, error) {
	raw, err := client.get(ctx, path)
	if errors.Is(err, errCatalogUnknown) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var body deezerListBody[T]
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, blocked("non-JSON deezer response for %s", path)
	}
	if body.Error != nil {
		return nil, fmt.Errorf("deezer error code %d on %s", body.Error.Code, path)
	}
	return body.Data, nil
}

func (d deezerArtistSource) FindArtists(ctx context.Context, name string) ([]providerArtist, error) {
	hits, err := deezerList[deezerArtistHit](ctx, d.client, "/2.0/search/artist?q="+url.QueryEscape(name)+"&limit="+strconv.Itoa(deezerArtistSearchLimit))
	if err != nil {
		return nil, err
	}
	artists := make([]providerArtist, 0, len(hits))
	for _, hit := range hits {
		artists = append(artists, providerArtist{ID: strconv.FormatInt(hit.ID, 10), Name: hit.Name})
	}
	return artists, nil
}

func (d deezerArtistSource) Catalog(ctx context.Context, artist providerArtist) (catalogView, error) {
	base := "/2.0/artist/" + url.PathEscape(artist.ID)
	albums, err := deezerList[deezerTitleHit](ctx, d.client, base+"/albums?limit="+strconv.Itoa(deezerAlbumLimit))
	if err != nil {
		return catalogView{}, err
	}
	top, err := deezerList[deezerTitleHit](ctx, d.client, base+"/top?limit="+strconv.Itoa(deezerTopTrackLimit))
	if err != nil {
		return catalogView{}, err
	}
	return catalogView{Albums: titlesOf(albums), Titles: titlesOf(top)}, nil
}

func titlesOf(hits []deezerTitleHit) []string {
	titles := make([]string, 0, len(hits))
	for _, hit := range hits {
		titles = append(titles, hit.Title)
	}
	return titles
}

func (d deezerArtistSource) Songs(ctx context.Context, artist providerArtist, _ catalogView, t libraryTrack, keys domain.SearchKeys) ([]candidate, error) {
	query := `artist:"` + withoutQuotes(keys.Artist) + `" track:"` + withoutQuotes(keys.Title) + `"`
	hits, err := deezerList[deezerSongHit](ctx, d.client, "/2.0/search/track?q="+url.QueryEscape(query)+"&limit="+strconv.Itoa(deezerSongSearchLimit))
	if err != nil {
		return nil, err
	}
	plausible := plausibleHits(hits, artist, t)
	if len(plausible) > maxExpandedSongs {
		return nil, errTooManyMatches
	}
	var found []candidate
	for _, hit := range plausible {
		full, err := d.fullTrack(ctx, hit.ID)
		if err != nil {
			return nil, err
		}
		found = append(found, full...)
	}
	return found, nil
}

func plausibleHits(hits []deezerSongHit, artist providerArtist, t libraryTrack) []deezerSongHit {
	return slices.DeleteFunc(slices.Clone(hits), func(hit deezerSongHit) bool {
		return strconv.FormatInt(hit.Artist.ID, 10) != artist.ID || !isPlausible(t, deezerHitCandidate(hit))
	})
}

func deezerHitCandidate(hit deezerSongHit) candidate {
	return candidate{
		ID: strconv.FormatInt(hit.ID, 10), Title: hit.Title, Artists: []string{hit.Artist.Name},
		Album: hit.Album.Title, LengthSeconds: float64(hit.Duration),
	}
}

func (d deezerArtistSource) fullTrack(ctx context.Context, id int64) ([]candidate, error) {
	raw, err := d.client.get(ctx, "/2.0/track/"+strconv.FormatInt(id, 10))
	if errors.Is(err, errCatalogUnknown) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var body deezerTrackBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, blocked("non-JSON deezer response for track %d", id)
	}
	if body.Error != nil {
		return nil, fmt.Errorf("deezer error code %d", body.Error.Code)
	}
	return []candidate{deezerCandidate(body)}, nil
}

func withoutQuotes(text string) string { return strings.ReplaceAll(text, `"`, " ") }
