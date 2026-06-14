package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"altune/go-api/internal/discovery/domain"
)

type MusicBrainzAdapter struct {
	client    *http.Client
	userAgent string
}

func NewMusicBrainzAdapter(client *http.Client, userAgent string) *MusicBrainzAdapter {
	return &MusicBrainzAdapter{client: client, userAgent: userAgent}
}

func (a *MusicBrainzAdapter) Name() domain.ProviderName { return domain.ProviderMusicBrainz }

func (a *MusicBrainzAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *MusicBrainzAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	var results []domain.SearchResult

	for kind := range kinds {
		if !a.SupportedKinds()[kind] {
			continue
		}

		entity := mbEntity(kind)
		u := fmt.Sprintf("https://musicbrainz.org/ws/2/%s/?query=%s&fmt=json&limit=10",
			entity, url.QueryEscape(query))

		req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", a.userAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := a.client.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			continue
		}

		switch kind {
		case domain.ResultKindTrack:
			var body mbRecordingResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				continue
			}
			for _, rec := range body.Recordings {
				sr := mapMBRecording(rec)
				results = append(results, sr)
			}
		case domain.ResultKindArtist:
			var body mbArtistResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				continue
			}
			for _, art := range body.Artists {
				sr := mapMBArtist(art)
				results = append(results, sr)
			}
		case domain.ResultKindAlbum:
			var body mbReleaseGroupResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				continue
			}
			for _, rg := range body.ReleaseGroups {
				sr := mapMBReleaseGroup(rg)
				results = append(results, sr)
			}
		}
	}

	return results, nil
}

func mbEntity(kind domain.ResultKind) string {
	switch kind {
	case domain.ResultKindTrack:
		return "recording"
	case domain.ResultKindAlbum:
		return "release-group"
	case domain.ResultKindArtist:
		return "artist"
	default:
		return "recording"
	}
}

func mapMBRecording(rec mbRecording) domain.SearchResult {
	extras := make(map[string]any)
	extras["mbid"] = rec.ID
	if len(rec.ISRCs) > 0 {
		extras["isrc"] = rec.ISRCs[0]
	}

	var subtitle string
	if len(rec.ArtistCredit) > 0 {
		subtitle = rec.ArtistCredit[0].Name
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindTrack,
		Title:      rec.Title,
		Subtitle:   subtitle,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderMusicBrainz,
			ExternalID: rec.ID,
			URL:        "https://musicbrainz.org/recording/" + rec.ID,
		}},
		Extras: extras,
	}
}

func mapMBArtist(art mbArtistItem) domain.SearchResult {
	extras := make(map[string]any)
	extras["mbid"] = art.ID

	return domain.SearchResult{
		Kind:       domain.ResultKindArtist,
		Title:      art.Name,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderMusicBrainz,
			ExternalID: art.ID,
			URL:        "https://musicbrainz.org/artist/" + art.ID,
		}},
		Extras: extras,
	}
}

func mapMBReleaseGroup(rg mbReleaseGroup) domain.SearchResult {
	extras := make(map[string]any)
	extras["mbid"] = rg.ID

	var subtitle string
	if len(rg.ArtistCredit) > 0 {
		subtitle = rg.ArtistCredit[0].Name
	}

	return domain.SearchResult{
		Kind:       domain.ResultKindAlbum,
		Title:      rg.Title,
		Subtitle:   subtitle,
		Confidence: domain.ConfidenceLow,
		Sources: []domain.SourceRef{{
			Provider:   domain.ProviderMusicBrainz,
			ExternalID: rg.ID,
			URL:        "https://musicbrainz.org/release-group/" + rg.ID,
		}},
		Extras: extras,
	}
}

type mbRecordingResponse struct {
	Recordings []mbRecording `json:"recordings"`
}

type mbRecording struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	ISRCs        []string        `json:"isrcs"`
	ArtistCredit []mbArtistRef   `json:"artist-credit"`
}

type mbArtistRef struct {
	Name string `json:"name"`
}

type mbArtistResponse struct {
	Artists []mbArtistItem `json:"artists"`
}

type mbArtistItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type mbReleaseGroupResponse struct {
	ReleaseGroups []mbReleaseGroup `json:"release-groups"`
}

type mbReleaseGroup struct {
	ID           string        `json:"id"`
	Title        string        `json:"title"`
	ArtistCredit []mbArtistRef `json:"artist-credit"`
}
