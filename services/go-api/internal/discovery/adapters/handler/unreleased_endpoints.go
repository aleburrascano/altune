package handler

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/unreleased"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

type UnreleasedSongDTO struct {
	GeniusID    int                `json:"genius_id"`
	Title       string             `json:"title"`
	CoverURL    *string            `json:"cover_url"`
	GeniusURL   string             `json:"genius_url"`
	ReleaseDate *string            `json:"release_date"`
	HasMedia    *bool              `json:"has_media"`
	AudioLinks  []domain.AudioLink `json:"audio_links"`
}

type UnreleasedResponseDTO struct {
	Items   []UnreleasedSongDTO `json:"items"`
	Pending bool                `json:"pending"`
}

func (h *DiscoveryHandler) WithUnreleased(svc *unreleased.Service) *DiscoveryHandler {
	h.unreleasedSvc = svc
	return h
}

func unreleasedToDTO(found unreleased.Lookup) UnreleasedResponseDTO {
	items := make([]UnreleasedSongDTO, len(found.Songs))
	for i, s := range found.Songs {
		items[i] = UnreleasedSongDTO{
			GeniusID:    s.GeniusID,
			Title:       s.Title,
			CoverURL:    nonEmpty(s.CoverURL),
			GeniusURL:   s.GeniusURL,
			ReleaseDate: nonEmpty(s.ReleaseDate),
			HasMedia:    s.HasMedia,
			AudioLinks:  nonNilLinks(s.AudioLinks),
		}
	}
	return UnreleasedResponseDTO{Items: items, Pending: found.Pending}
}

func nonNilLinks(links []domain.AudioLink) []domain.AudioLink {
	if links == nil {
		return []domain.AudioLink{}
	}
	return links
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (h *DiscoveryHandler) handleArtistUnreleased(w http.ResponseWriter, r *http.Request) {
	withProvider(w, r, true,
		nil,
		func(pn domain.ProviderName, provider, externalID string) {
			name, ok := textParam(w, r, "name")
			if !ok {
				return
			}
			if h.unreleasedSvc == nil {
				httputil.WriteJSON(w, http.StatusOK, unreleasedToDTO(unreleased.Lookup{}))
				return
			}
			found, err := h.unreleasedSvc.Lookup(r.Context(), pn, externalID, name, knownIDsParam(r))
			if err != nil {
				failContentFetch(w, r, err, "get artist unreleased failed", provider, externalID)
				return
			}
			httputil.WriteJSON(w, http.StatusOK, unreleasedToDTO(found))
		})
}
