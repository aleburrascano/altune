package handler

import (
	"altune/go-api/internal/discovery/adapters/authbridge"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/ownership"
	"altune/go-api/internal/discovery/service/unreleased"
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
)

type UnreleasedArtistDTO struct {
	Provider   string `json:"provider"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
}

type UnreleasedSearchItemDTO struct {
	UnreleasedSongDTO
	Artist UnreleasedArtistDTO `json:"artist"`
}

type UnreleasedSearchResponseDTO struct {
	Items []UnreleasedSearchItemDTO `json:"items"`
}

func unreleasedSearchToDTO(hits []ports.UnreleasedHit) UnreleasedSearchResponseDTO {
	items := make([]UnreleasedSearchItemDTO, len(hits))
	for i, hit := range hits {
		items[i] = UnreleasedSearchItemDTO{
			UnreleasedSongDTO: unreleasedSongToDTO(hit.Song),
			Artist:            UnreleasedArtistDTO{Provider: hit.Artist.Provider.String(), ExternalID: hit.Artist.ExternalID, Name: hit.ArtistName},
		}
	}
	return UnreleasedSearchResponseDTO{Items: items}
}

func (h *DiscoveryHandler) handleUnreleasedSearch(w http.ResponseWriter, r *http.Request) {
	query, ok := textParam(w, r, "q")
	if !ok {
		return
	}
	hits := []ports.UnreleasedHit{}
	if h.unreleasedSvc != nil {
		found, err := h.unreleasedSvc.Search(r.Context(), query)
		if err != nil {
			slog.ErrorContext(r.Context(), "search unreleased failed", "error", err)
			httputil.HandleServiceError(w, r, err)
			return
		}
		hits = found
	}
	dto := unreleasedSearchToDTO(hits)
	if userId, ok := authbridge.UserIDFromContext(r.Context()); ok {
		h.ownership.StampOwnership(r.Context(), userId, searchOwnableItems(dto.Items))
	}
	httputil.WriteJSON(w, http.StatusOK, dto)
}

func searchOwnableItems(items []UnreleasedSearchItemDTO) []ownership.OwnableItem {
	var out []ownership.OwnableItem
	for i := range items {
		out = append(out, ownableSongItems(items[i].Artist.Name, &items[i].UnreleasedSongDTO)...)
	}
	return out
}

type UnreleasedSongDTO struct {
	GeniusID         *int                   `json:"genius_id"`
	Title            string                 `json:"title"`
	CoverURL         *string                `json:"cover_url"`
	GeniusURL        *string                `json:"genius_url"`
	ReleaseDate      *string                `json:"release_date"`
	HasMedia         *bool                  `json:"has_media"`
	Quality          *string                `json:"quality"`
	QualityRank      int                    `json:"quality_rank"`
	TrackerKey       *string                `json:"tracker_key"`
	OffStreaming     bool                   `json:"off_streaming"`
	OpenURL          *string                `json:"open_url,omitempty"`
	OpenHost         *string                `json:"open_host,omitempty"`
	Versions         []UnreleasedVersionDTO `json:"versions,omitempty"`
	AudioLinks       []domain.AudioLink     `json:"audio_links"`
	Extras           map[string]any         `json:"extras"`
	BestVersionTitle string                 `json:"best_version_title"`
}

type UnreleasedVersionDTO struct {
	domain.UnreleasedVersion
	Extras map[string]any `json:"extras"`
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
		items[i] = unreleasedSongToDTO(s)
	}
	return UnreleasedResponseDTO{Items: items, Pending: found.Pending}
}

func unreleasedSongToDTO(s domain.UnreleasedSong) UnreleasedSongDTO {
	return UnreleasedSongDTO{
		BestVersionTitle: ownedTitle(s),
		GeniusID:         nonZero(s.GeniusID),
		Title:            s.Title,
		CoverURL:         nonEmpty(s.CoverURL),
		GeniusURL:        nonEmpty(s.GeniusURL),
		ReleaseDate:      nonEmpty(s.ReleaseDate),
		HasMedia:         s.HasMedia,
		Quality:          nonEmpty(s.Quality),
		QualityRank:      s.QualityRank(),
		TrackerKey:       nonEmpty(s.TrackerKey),
		OffStreaming:     s.OffStreaming,
		OpenURL:          nonEmpty(s.OpenURL),
		OpenHost:         nonEmpty(s.OpenHost),
		Versions:         versionDTOs(s.Versions),
		AudioLinks:       nonNilLinks(s.AudioLinks),
		Extras:           map[string]any{},
	}
}

func nonNilLinks(links []domain.AudioLink) []domain.AudioLink {
	if links == nil {
		return []domain.AudioLink{}
	}
	return links
}

func ownedTitle(s domain.UnreleasedSong) string {
	if len(s.Versions) == 0 {
		return s.Title
	}
	return s.BestVersion().Title
}

func versionDTOs(versions []domain.UnreleasedVersion) []UnreleasedVersionDTO {
	out := make([]UnreleasedVersionDTO, len(versions))
	for i, v := range versions {
		out[i] = UnreleasedVersionDTO{UnreleasedVersion: v, Extras: map[string]any{}}
	}
	return out
}

func unreleasedOwnableItems(artistName string, items []UnreleasedSongDTO) []ownership.OwnableItem {
	var out []ownership.OwnableItem
	for i := range items {
		out = append(out, ownableSongItems(artistName, &items[i])...)
	}
	return out
}

func ownableSongItems(artistName string, item *UnreleasedSongDTO) []ownership.OwnableItem {
	track := domain.ResultKindTrack.String()
	out := []ownership.OwnableItem{{Kind: track, Title: item.BestVersionTitle, Artist: artistName, Extras: &item.Extras}}
	for j := range item.Versions {
		v := &item.Versions[j]
		out = append(out, ownership.OwnableItem{Kind: track, Title: v.Title, Artist: artistName, Extras: &v.Extras})
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
