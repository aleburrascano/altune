package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/service"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"strconv"
	"strings"
	"time"

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

// librarySearchTerm trims and bounds the q parameter every list endpoint in
// this package accepts. The term reaches an ILIKE comparison against Postgres
// text, so it is held to the same NUL-byte refusal as a stored field.
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

// pageBounds reads the limit/offset window every list endpoint in this package
// accepts. An absent or unparseable bound comes back as zero, which the services
// read as "the caller named none" and answer with their default page.
func pageBounds(r *http.Request) (limit, offset int) {
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ = strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, offset
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

	items := httputil.MapSlice(albums, func(a domain.AlbumGroup) AlbumGroupDTO {
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
	httputil.WriteJSON(w, http.StatusOK, httputil.NewList(items))
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

	items := httputil.MapSlice(artists, func(a domain.ArtistGroup) ArtistGroupDTO {
		return ArtistGroupDTO{
			Key:               a.Key,
			Artist:            a.Artist,
			ArtworkURL:        a.ArtworkURL,
			TrackCount:        a.TrackCount,
			MostRecentAddedAt: formatAddedAt(a.MostRecentAddedAt),
		}
	})
	httputil.WriteJSON(w, http.StatusOK, httputil.NewList(items))
}
