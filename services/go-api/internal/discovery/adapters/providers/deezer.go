package providers

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type DeezerAdapter struct {
	client *http.Client
}

func NewDeezerAdapter(client *http.Client) *DeezerAdapter {
	return &DeezerAdapter{client: client}
}

func (a *DeezerAdapter) Name() domain.ProviderName { return domain.ProviderDeezer }

func (a *DeezerAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return allSearchKinds()
}

func (a *DeezerAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return searchAcrossKinds(ctx, a.Name().String(), query, kinds, a.SupportedKinds(),
		func(ctx context.Context, kind domain.ResultKind) ([]domain.SearchResult, error) {
			return a.searchKind(ctx, query, kind)
		})
}

func (a *DeezerAdapter) SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult
	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}
		q := deezerStructuredQuery(artist, track, kind)
		items, err := a.searchKind(ctx, q, kind)
		if err != nil {
			continue
		}
		results = append(results, items...)
	}
	return results, nil
}

func deezerStripQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, "")
}

func deezerStructuredQuery(artist, track string, kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		//nolint:gocritic // Deezer search DSL needs literal double-quotes; %q would apply Go escaping (\u, \t) and corrupt the query
		return fmt.Sprintf(`artist:"%s" track:"%s"`, deezerStripQuotes(artist), deezerStripQuotes(track))
	case domain.ResultKindAlbum:
		//nolint:gocritic // Deezer search DSL needs literal double-quotes; %q would apply Go escaping (\u, \t) and corrupt the query
		return fmt.Sprintf(`artist:"%s" album:"%s"`, deezerStripQuotes(artist), deezerStripQuotes(track))
	case domain.ResultKindArtist:
		return artist
	default:
		return artist + " " + track
	}
}

func (a *DeezerAdapter) searchKind(ctx context.Context, query string, kind domain.ResultKind) ([]domain.SearchResult, error) {
	endpoint := deezerSearchEndpoint(kind)
	if endpoint == "" {
		return nil, fmt.Errorf("unsupported kind")
	}

	u := fmt.Sprintf("https://api.deezer.com/search/%s?q=%s&limit=15&order=RANKING", endpoint, url.QueryEscape(query))

	var body deezerSearchResponse
	if err := a.getJSON(ctx, u, &body); err != nil {
		return nil, err
	}

	var results []domain.SearchResult
	for _, item := range body.Data {
		results = append(results, mapDeezerResult(item, kind))
	}
	return results, nil
}

func deezerSearchEndpoint(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "track"
	case domain.ResultKindAlbum:
		return "album"
	case domain.ResultKindArtist:
		return "artist"
	default:
		return ""
	}
}

func preferURL(hi, lo string) string {
	if hi != "" {
		return hi
	}
	return lo
}

func mapDeezerResult(item deezerItem, kind domain.ResultKind) domain.SearchResult {
	var title, subtitle, imageURL string
	extras := make(map[string]any)

	switch kind {
	case domain.ResultKindTrack:
		title = item.Title
		if item.Artist != nil {
			subtitle = item.Artist.Name
		}
		if item.Album != nil {
			imageURL = preferURL(item.Album.CoverXL, item.Album.CoverBig)
			extras[domain.ExtraAlbum] = item.Album.Title
		}
		extras[domain.ExtraDuration] = item.Duration
		if item.Preview != "" {
			extras[domain.ExtraPreviewURL] = item.Preview
		}
		if item.ExplicitLyrics {
			extras[domain.ExtraExplicit] = true
		}
	case domain.ResultKindAlbum:
		title = item.Title
		if item.Artist != nil {
			subtitle = item.Artist.Name
		}
		imageURL = preferURL(item.CoverXL, item.CoverBig)
		if item.GenreID > 0 {
			extras["genre_id"] = item.GenreID
		}
	case domain.ResultKindArtist:
		title = item.Name
		imageURL = preferURL(item.PictureXL, item.PictureBig)
	}

	r := domain.NewProviderResult(kind, title, subtitle, imageURL,
		domain.SourceRef{Provider: domain.ProviderDeezer, ExternalID: fmt.Sprintf("%d", item.ID), URL: item.Link},
		extras)
	if kind == domain.ResultKindTrack {
		r.ISRC = item.ISRC
		r.ProviderRank = item.Rank
		if item.Album != nil {
			r.Album = item.Album.Title
			r.DeezerAlbumID = fmt.Sprintf("%d", item.Album.ID)
		}
		r.Duration = item.Duration
	}
	if kind == domain.ResultKindAlbum {
		r.RecordType = domain.RecordType(item.RecordType)
		r.ReleaseDate = item.ReleaseDate
		r.TrackCount = item.NbTracks
	}
	r.FanCount = item.NbFan
	return r
}

type deezerAPIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func (e *deezerAPIError) Error() string {
	return fmt.Sprintf("deezer api error %d (%s): %s", e.Code, e.Type, e.Message)
}

type deezerSearchResponse struct {
	Data        []deezerItem `json:"data"`
	NextPageURL string       `json:"next"`
}

type deezerItem struct {
	ID             int64        `json:"id"`
	Title          string       `json:"title"`
	Name           string       `json:"name"`
	Link           string       `json:"link"`
	Preview        string       `json:"preview"`
	Duration       int          `json:"duration"`
	ISRC           string       `json:"isrc"`
	CoverBig       string       `json:"cover_big"`
	CoverXL        string       `json:"cover_xl"`
	PictureBig     string       `json:"picture_big"`
	PictureXL      string       `json:"picture_xl"`
	Artist         *deezerRef   `json:"artist"`
	Album          *deezerAlbum `json:"album"`
	ExplicitLyrics bool         `json:"explicit_lyrics"`
	RecordType     string       `json:"record_type"`
	ReleaseDate    string       `json:"release_date"`
	NbTracks       int          `json:"nb_tracks"`
	Rank           int64        `json:"rank"`
	NbFan          int64        `json:"nb_fan"`
	GenreID        int          `json:"genre_id"`
}

type deezerRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type deezerAlbum struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	CoverBig string `json:"cover_big"`
	CoverXL  string `json:"cover_xl"`
}
