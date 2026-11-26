package handler

import (
	"altune/go-api/internal/library/domain"
	libraryservice "altune/go-api/internal/library/internal/core/libraryviews"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

type LibraryHandler struct {
	lenses *libraryservice.LibraryViewService
}

func NewLibraryHandler(lenses *libraryservice.LibraryViewService) *LibraryHandler {
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

func libraryQuery(r *http.Request) (domain.LibraryQuery, error) {
	sort, err := domain.ParseLibrarySort(r.URL.Query().Get("sort"))
	if err != nil {
		return domain.LibraryQuery{}, err
	}
	search, err := librarySearchTerm(r.URL.Query().Get("q"))
	if err != nil {
		return domain.LibraryQuery{}, err
	}
	limit, offset := pageBounds(r)
	return domain.LibraryQuery{
		Search: search,
		Sort:   sort,
		Limit:  limit,
		Offset: offset,
	}, nil
}

func librarySearchTerm(raw string) (string, error) {
	search := strings.TrimSpace(raw)
	if err := domain.ValidateText(search, "search term"); err != nil {
		return "", err
	}
	if len(search) > domain.MaxLibrarySearchLength {
		return "", domain.NewValidationError(
			"search term exceeds " + strconv.Itoa(domain.MaxLibrarySearchLength) + " characters")
	}
	return search, nil
}

func pageBounds(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, offset
}

func formatAddedAt(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func (h *LibraryHandler) handleAlbums(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}
	query, err := libraryQuery(r)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	albums, err := h.lenses.Albums(r.Context(), userId, query)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	items := httpserver.MapSlice(albums, func(a domain.AlbumGroup) AlbumGroupDTO {
		return AlbumGroupDTO{
			Key:               a.Key,
			Album:             a.Album,
			Artist:            a.Artist,
			ArtworkURL:        a.ArtworkURL,
			Year:              a.Year,
			TrackCount:        a.TrackCount,
			MostRecentAddedAt: formatAddedAt(a.MostRecentAddedAt),
		}
	})
	httpserver.WriteJSON(w, http.StatusOK, httpserver.NewList(items))
}

func (h *LibraryHandler) handleArtists(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}
	query, err := libraryQuery(r)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	artists, err := h.lenses.Artists(r.Context(), userId, query)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	items := httpserver.MapSlice(artists, func(a domain.ArtistGroup) ArtistGroupDTO {
		return ArtistGroupDTO{
			Key:               a.Key,
			Artist:            a.Artist,
			ArtworkURL:        a.ArtworkURL,
			TrackCount:        a.TrackCount,
			MostRecentAddedAt: formatAddedAt(a.MostRecentAddedAt),
		}
	})
	httpserver.WriteJSON(w, http.StatusOK, httpserver.NewList(items))
}
