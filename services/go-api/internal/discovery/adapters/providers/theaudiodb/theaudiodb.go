package theaudiodb

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"net/http"
	"net/url"
)

type Adapter struct {
	client *http.Client
}

func New(client *http.Client) *Adapter {
	return &Adapter{client: client}
}

func (a *Adapter) Name() domain.ProviderName { return domain.ProviderTheAudioDB }

func (a *Adapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	}
}

func (a *Adapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if !kinds[domain.ResultKindArtist] {
		return nil, nil
	}

	u := fmt.Sprintf("https://theaudiodb.com/api/v1/json/523532/search.php?s=%s", url.QueryEscape(query))

	var body struct {
		Artists []artistHit `json:"artists"`
	}
	if err := providerhttp.GetJSON(ctx, a.client, u, &body); err != nil {
		return nil, err
	}

	return artistResults(body.Artists), nil
}

type artistHit struct {
	IDArtist       string `json:"idArtist"`
	StrArtist      string `json:"strArtist"`
	StrArtistThumb string `json:"strArtistThumb"`
}

func artistResults(artists []artistHit) []domain.SearchResult {
	var results []domain.SearchResult
	for _, art := range artists {
		results = append(results, domain.NewProviderResult(domain.ResultKindArtist, art.StrArtist, "", art.StrArtistThumb,
			domain.SourceRef{Provider: domain.ProviderTheAudioDB, ExternalID: art.IDArtist},
			nil))
	}
	return results
}

func (a *Adapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if kind == domain.ResultKindArtist {
		return a.resolveArtist(ctx, title, mbid)
	}
	return a.resolveAlbum(ctx, title, subtitle)
}

func (a *Adapter) resolveArtist(ctx context.Context, title, mbid string) (string, error) {
	if mbid != "" {
		if art := a.artistThumbByMBID(ctx, mbid); art != "" {
			return art, nil
		}
	}
	results, err := a.Search(ctx, title, map[domain.ResultKind]bool{domain.ResultKindArtist: true})
	if err != nil {
		return providerhttp.ArtworkFailure(a.ArtworkSource(), err)
	}
	if len(results) == 0 {
		return "", nil
	}
	return results[0].ImageURL, nil
}

func (a *Adapter) resolveAlbum(ctx context.Context, title, subtitle string) (string, error) {
	if subtitle == "" {
		return "", nil
	}

	u := fmt.Sprintf("https://theaudiodb.com/api/v1/json/523532/searchalbum.php?s=%s&a=%s",
		url.QueryEscape(subtitle), url.QueryEscape(title))
	var body struct {
		Album []struct {
			StrAlbumThumb string `json:"strAlbumThumb"`
		} `json:"album"`
	}
	if err := providerhttp.GetJSON(ctx, a.client, u, &body); err != nil {
		return providerhttp.ArtworkFailure(a.ArtworkSource(), err)
	}
	if len(body.Album) > 0 && body.Album[0].StrAlbumThumb != "" {
		return body.Album[0].StrAlbumThumb, nil
	}
	return "", nil
}

func (a *Adapter) artistThumbByMBID(ctx context.Context, mbid string) string {
	u := fmt.Sprintf("https://theaudiodb.com/api/v1/json/523532/artist-mb.php?i=%s", url.QueryEscape(mbid))
	var body struct {
		Artists []struct {
			StrArtistThumb string `json:"strArtistThumb"`
		} `json:"artists"`
	}
	if err := providerhttp.GetJSON(ctx, a.client, u, &body); err != nil {
		return ""
	}
	if len(body.Artists) > 0 {
		return body.Artists[0].StrArtistThumb
	}
	return ""
}

func (*Adapter) ArtworkSource() domain.ProviderKey { return domain.ProviderKeyTheAudioDB }
