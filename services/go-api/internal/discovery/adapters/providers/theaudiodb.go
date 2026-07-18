package providers

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"altune/go-api/internal/discovery/domain"
)

type TheAudioDBAdapter struct {
	client *http.Client
}

func NewTheAudioDBAdapter(client *http.Client) *TheAudioDBAdapter {
	return &TheAudioDBAdapter{client: client}
}

func (a *TheAudioDBAdapter) Name() domain.ProviderName { return domain.ProviderTheAudioDB }

func (a *TheAudioDBAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindArtist: true,
	}
}

func (a *TheAudioDBAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if !kinds[domain.ResultKindArtist] {
		return nil, nil
	}

	u := fmt.Sprintf("https://theaudiodb.com/api/v1/json/523532/search.php?s=%s", url.QueryEscape(query))

	var body struct {
		Artists []struct {
			IDArtist       string `json:"idArtist"`
			StrArtist      string `json:"strArtist"`
			StrArtistThumb string `json:"strArtistThumb"`
		} `json:"artists"`
	}
	if err := getJSON(ctx, a.client, u, &body); err != nil {
		return nil, nil
	}

	var results []domain.SearchResult
	for _, art := range body.Artists {
		results = append(results, domain.NewProviderResult(domain.ResultKindArtist, art.StrArtist, "", art.StrArtistThumb,
			domain.SourceRef{Provider: domain.ProviderTheAudioDB, ExternalID: art.IDArtist},
			nil))
	}

	return results, nil
}

// Resolve implements ArtworkResolver — searches TheAudioDB for album/artist covers.
func (a *TheAudioDBAdapter) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle string, mbid string) (string, error) {
	if kind == domain.ResultKindArtist {
		// Prefer the deterministic MBID lookup over name-fuzzing when the caller
		// supplies an mbid — the free key's 1-result name search is ambiguous, but
		// artist-mb.php?i={mbid} resolves by identity. (artist-mb.php confirmed on
		// the free key, 2026-06-22.)
		if mbid != "" {
			if art := a.artistThumbByMBID(ctx, mbid); art != "" {
				return art, nil
			}
		}
		results, err := a.Search(ctx, title, map[domain.ResultKind]bool{domain.ResultKindArtist: true})
		if err != nil || len(results) == 0 {
			return "", nil
		}
		if results[0].ImageURL != "" {
			return results[0].ImageURL, nil
		}
		return "", nil
	}

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
	if err := getJSON(ctx, a.client, u, &body); err != nil {
		return "", nil
	}
	if len(body.Album) > 0 && body.Album[0].StrAlbumThumb != "" {
		return body.Album[0].StrAlbumThumb, nil
	}
	return "", nil
}

// artistThumbByMBID resolves an artist's thumbnail by MusicBrainz id via
// artist-mb.php — identity-keyed, no name fuzzing or 1-result-cap ambiguity.
func (a *TheAudioDBAdapter) artistThumbByMBID(ctx context.Context, mbid string) string {
	u := fmt.Sprintf("https://theaudiodb.com/api/v1/json/523532/artist-mb.php?i=%s", url.QueryEscape(mbid))
	var body struct {
		Artists []struct {
			StrArtistThumb string `json:"strArtistThumb"`
		} `json:"artists"`
	}
	if err := getJSON(ctx, a.client, u, &body); err != nil {
		return ""
	}
	if len(body.Artists) > 0 {
		return body.Artists[0].StrArtistThumb
	}
	return ""
}

func (*TheAudioDBAdapter) ArtworkSource() string { return "theaudiodb" }
