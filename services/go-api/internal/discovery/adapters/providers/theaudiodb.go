package providers

import (
	"context"
	"encoding/json"
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

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, nil
	}

	var body struct {
		Artists []struct {
			IDArtist    string `json:"idArtist"`
			StrArtist   string `json:"strArtist"`
			StrArtistThumb string `json:"strArtistThumb"`
		} `json:"artists"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, nil
	}

	var results []domain.SearchResult
	for _, art := range body.Artists {
		results = append(results, domain.SearchResult{
			Kind:       domain.ResultKindArtist,
			Title:      art.StrArtist,
			ImageURL:   art.StrArtistThumb,
			Confidence: domain.ConfidenceLow,
			Sources: []domain.SourceRef{{
				Provider:   domain.ProviderTheAudioDB,
				ExternalID: art.IDArtist,
			}},
			Extras: make(map[string]any),
		})
	}

	return results, nil
}
