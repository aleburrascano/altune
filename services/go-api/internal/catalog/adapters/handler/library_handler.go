package handler

import (
	"net/http"
	"strings"
	"time"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"

	"github.com/go-chi/chi/v5"
)

type LibraryHandler struct {
	lenses *service.LibraryLensService
}

func NewLibraryHandler(lenses *service.LibraryLensService) *LibraryHandler {
	return &LibraryHandler{lenses: lenses}
}

func (h *LibraryHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/albums", h.handleAlbums)
	r.Get("/artists", h.handleArtists)
	return r
}

type AlbumGroupDTO struct {
	Key               string  `json:"key"`
	Album             string  `json:"album"`
	Artist            string  `json:"artist"`
	ArtworkURL        *string `json:"artwork_url"`
	Year              *int    `json:"year"`
	TrackCount        int     `json:"track_count"`
	MostRecentAddedAt string  `json:"most_recent_added_at"`
}

type ArtistGroupDTO struct {
	Key               string  `json:"key"`
	Artist            string  `json:"artist"`
	ArtworkURL        *string `json:"artwork_url"`
	TrackCount        int     `json:"track_count"`
	MostRecentAddedAt string  `json:"most_recent_added_at"`
}

type ListAlbumsResponse struct {
	Items []AlbumGroupDTO `json:"items"`
	Total int             `json:"total"`
}

type ListArtistsResponse struct {
	Items []ArtistGroupDTO `json:"items"`
	Total int              `json:"total"`
}

func libraryQuery(r *http.Request) (domain.LibraryQuery, error) {
	sort, err := domain.ParseLibrarySort(r.URL.Query().Get("sort"))
	if err != nil {
		return domain.LibraryQuery{}, err
	}
	return domain.LibraryQuery{
		Search: strings.TrimSpace(r.URL.Query().Get("q")),
		Sort:   sort,
	}, nil
}

func formatAddedAt(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func (h *LibraryHandler) handleAlbums(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	query, err := libraryQuery(r)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	albums, err := h.lenses.Albums(r.Context(), userId, query)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	items := make([]AlbumGroupDTO, len(albums))
	for i, a := range albums {
		items[i] = AlbumGroupDTO{
			Key:               a.Key,
			Album:             a.Album,
			Artist:            a.Artist,
			ArtworkURL:        a.ArtworkURL,
			Year:              a.Year,
			TrackCount:        a.TrackCount,
			MostRecentAddedAt: formatAddedAt(a.MostRecentAddedAt),
		}
	}
	httputil.WriteJSON(w, http.StatusOK, ListAlbumsResponse{Items: items, Total: len(items)})
}

func (h *LibraryHandler) handleArtists(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}
	query, err := libraryQuery(r)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	artists, err := h.lenses.Artists(r.Context(), userId, query)
	if err != nil {
		httputil.HandleServiceError(w, r, err)
		return
	}

	items := make([]ArtistGroupDTO, len(artists))
	for i, a := range artists {
		items[i] = ArtistGroupDTO{
			Key:               a.Key,
			Artist:            a.Artist,
			ArtworkURL:        a.ArtworkURL,
			TrackCount:        a.TrackCount,
			MostRecentAddedAt: formatAddedAt(a.MostRecentAddedAt),
		}
	}
	httputil.WriteJSON(w, http.StatusOK, ListArtistsResponse{Items: items, Total: len(items)})
}
