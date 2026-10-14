package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

const (
	deezerBaseURL        = "https://api.deezer.com"
	deezerUserAgent      = "altune-libraryderive"
	deezerRequestsPerSec = 2.0
	deezerQuotaCode      = 4
	deezerNoDataCode     = 800
	deezerAlbumLimit     = 10
)

type deezer struct{ client *catalogClient }

func newDeezer(httpClient *http.Client, baseURL string, requestsPerSecond float64) deezer {
	return deezer{client: newCatalogClient(httpClient, baseURL, deezerUserAgent, requestsPerSecond, isDeezerThrottle)}
}

type deezerErrorBody struct {
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func isDeezerThrottle(status int, body []byte) bool {
	var parsed deezerErrorBody
	return status == http.StatusTooManyRequests || (json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Code == deezerQuotaCode)
}

type deezerTrackBody struct {
	deezerErrorBody
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	ISRC     string `json:"isrc"`
	Duration int    `json:"duration"`
	Position int    `json:"track_position"`
	Album    struct {
		ID    int64  `json:"id"`
		Title string `json:"title"`
	} `json:"album"`
}

type deezerAlbumBody struct {
	deezerErrorBody
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	CoverXL string `json:"cover_xl"`
	Cover   string `json:"cover_big"`
	Genres  struct {
		Data []struct {
			Name string `json:"name"`
		} `json:"data"`
	} `json:"genres"`
	Tracks struct {
		Data []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
		} `json:"data"`
	} `json:"tracks"`
}

type deezerSearchBody struct {
	deezerErrorBody
	Data []deezerAlbumBody `json:"data"`
}

func (d deezer) TrackByISRC(ctx context.Context, isrc string) (deezerTrack, error) {
	return d.track(ctx, "/2.0/track/isrc:"+url.PathEscape(canonicalISRC(isrc)))
}

func (d deezer) TrackByID(ctx context.Context, id string) (deezerTrack, error) {
	return d.track(ctx, "/2.0/track/"+url.PathEscape(id))
}

func (d deezer) track(ctx context.Context, path string) (deezerTrack, error) {
	var body deezerTrackBody
	if err := d.getJSON(ctx, path, &body, &body.deezerErrorBody); err != nil {
		return deezerTrack{}, err
	}
	return deezerTrack{
		ID: strconv.FormatInt(body.ID, 10), ISRC: body.ISRC, Title: body.Title, Position: body.Position,
		DurationSeconds: float64(body.Duration), AlbumID: strconv.FormatInt(body.Album.ID, 10), AlbumTitle: body.Album.Title,
	}, nil
}

func (d deezer) SearchAlbums(ctx context.Context, artist, title string) ([]deezerAlbum, error) {
	query := `artist:"` + artist + `" album:"` + title + `"`
	var body deezerSearchBody
	if err := d.getJSON(ctx, "/2.0/search/album?q="+url.QueryEscape(query)+"&limit="+strconv.Itoa(deezerAlbumLimit), &body, &body.deezerErrorBody); err != nil {
		return nil, err
	}
	albums := make([]deezerAlbum, 0, len(body.Data))
	for _, item := range body.Data {
		albums = append(albums, toDeezerAlbum(item))
	}
	return albums, nil
}

func (d deezer) Album(ctx context.Context, id string) (deezerAlbum, error) {
	var body deezerAlbumBody
	if err := d.getJSON(ctx, "/2.0/album/"+url.PathEscape(id), &body, &body.deezerErrorBody); err != nil {
		return deezerAlbum{}, err
	}
	return toDeezerAlbum(body), nil
}

func toDeezerAlbum(body deezerAlbumBody) deezerAlbum {
	album := deezerAlbum{ID: strconv.FormatInt(body.ID, 10), Title: body.Title, CoverURL: cmp.Or(body.CoverXL, body.Cover)}
	if len(body.Genres.Data) > 0 {
		album.Genre = body.Genres.Data[0].Name
	}
	for _, track := range body.Tracks.Data {
		album.Tracks = append(album.Tracks, deezerAlbumTrack{ID: strconv.FormatInt(track.ID, 10), Title: track.Title})
	}
	return album
}

func (d deezer) getJSON(ctx context.Context, path string, into any, failure *deezerErrorBody) error {
	raw, err := d.client.get(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return blocked("non-JSON deezer response on %s", path)
	}
	switch {
	case failure.Error == nil:
		return nil
	case failure.Error.Code == deezerNoDataCode:
		return errCatalogUnknown
	}
	return fmt.Errorf("deezer error code %d on %s", failure.Error.Code, path)
}
