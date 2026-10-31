package handler

import (
	"altune/go-api/internal/discovery/adapters/authbridge"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/ownership"
	"altune/go-api/internal/discovery/service/unreleased"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

type UnreleasedSongDTO struct {
	GeniusID     *int                       `json:"genius_id"`
	Title        string                     `json:"title"`
	CoverURL     *string                    `json:"cover_url"`
	GeniusURL    *string                    `json:"genius_url"`
	ReleaseDate  *string                    `json:"release_date"`
	HasMedia     *bool                      `json:"has_media"`
	Quality      *string                    `json:"quality"`
	TrackerKey   *string                    `json:"tracker_key"`
	OffStreaming bool                       `json:"off_streaming"`
	OpenURL      *string                    `json:"open_url,omitempty"`
	OpenHost     *string                    `json:"open_host,omitempty"`
	Versions     []domain.UnreleasedVersion `json:"versions,omitempty"`
	AudioLinks   []domain.AudioLink         `json:"audio_links"`
	Extras       map[string]any             `json:"extras"`
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
			GeniusID:     nonZero(s.GeniusID),
			Title:        s.Title,
			CoverURL:     nonEmpty(s.CoverURL),
			GeniusURL:    nonEmpty(s.GeniusURL),
			ReleaseDate:  nonEmpty(s.ReleaseDate),
			HasMedia:     s.HasMedia,
			Quality:      nonEmpty(s.Quality),
			TrackerKey:   nonEmpty(s.TrackerKey),
			OffStreaming: s.OffStreaming,
			OpenURL:      nonEmpty(s.OpenURL),
			OpenHost:     nonEmpty(s.OpenHost),
			Versions:     s.Versions,
			AudioLinks:   nonNilLinks(s.AudioLinks),
			Extras:       map[string]any{},
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

func unreleasedOwnableItems(artistName string, items []UnreleasedSongDTO) []ownership.OwnableItem {
	out := make([]ownership.OwnableItem, len(items))
	for i := range items {
		out[i] = ownership.OwnableItem{
			Kind:   domain.ResultKindTrack.String(),
			Title:  items[i].Title,
			Artist: artistName,
			Extras: &items[i].Extras,
		}
	}
	return out
}

func (h *DiscoveryHandler) writeUnreleased(w http.ResponseWriter, r *http.Request, artistName string, found unreleased.Lookup) {
	dto := unreleasedToDTO(found)
	if userId, ok := authbridge.UserIDFromContext(r.Context()); ok {
		h.ownership.StampOwnership(r.Context(), userId, unreleasedOwnableItems(artistName, dto.Items))
	}
	httputil.WriteJSON(w, http.StatusOK, dto)
}

func nonZero(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
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
			h.writeUnreleased(w, r, name, found)
		})
}
