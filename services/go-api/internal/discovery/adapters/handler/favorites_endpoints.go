package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/httputil"
)

type FavoriteDTO struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type FavoritesResponse struct {
	Items []FavoriteDTO `json:"items"`
	Total int           `json:"total"`
}

type FavoriteRequest struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	ImageURL string `json:"image_url"`
}

func (h *DiscoveryHandler) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return
	}

	favorites, err := h.favoritesSvc.List(r.Context(), userId)
	if err != nil {
		slog.ErrorContext(r.Context(), "list favorites failed", "error", err)
		httputil.InternalError(w)
		return
	}

	items := make([]FavoriteDTO, len(favorites))
	for i, f := range favorites {
		items[i] = FavoriteDTO{
			Kind:     f.Kind.String(),
			Key:      f.Key,
			Title:    f.Title,
			Subtitle: f.Subtitle,
			ImageURL: f.ImageURL,
		}
	}

	httputil.WriteJSON(w, http.StatusOK, FavoritesResponse{Items: items, Total: len(items)})
}

func (h *DiscoveryHandler) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	userId, kind, req, ok := decodeFavorite(w, r)
	if !ok {
		return
	}

	fav := domain.Favorite{
		Kind:     kind,
		Title:    req.Title,
		Subtitle: req.Subtitle,
		ImageURL: req.ImageURL,
	}
	if err := h.favoritesSvc.Add(r.Context(), userId, fav); err != nil {
		slog.ErrorContext(r.Context(), "add favorite failed", "error", err)
		httputil.InternalError(w)
		return
	}

	httputil.WriteJSON(w, http.StatusOK, FavoriteDTO{
		Kind:     kind.String(),
		Key:      domain.FavoriteKey(kind, req.Title, req.Subtitle),
		Title:    req.Title,
		Subtitle: req.Subtitle,
		ImageURL: req.ImageURL,
	})
}

func (h *DiscoveryHandler) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	userId, kind, req, ok := decodeFavorite(w, r)
	if !ok {
		return
	}

	if err := h.favoritesSvc.Remove(r.Context(), userId, kind, req.Title, req.Subtitle); err != nil {
		slog.ErrorContext(r.Context(), "remove favorite failed", "error", err)
		httputil.InternalError(w)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func decodeFavorite(w http.ResponseWriter, r *http.Request) (shared.UserId, domain.ResultKind, FavoriteRequest, bool) {
	userId, ok := auth.RequireUserID(w, r)
	if !ok {
		return userId, domain.ResultKindUnknown, FavoriteRequest{}, false
	}

	var req FavoriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.BadRequest(w, "invalid request body")
		return userId, domain.ResultKindUnknown, req, false
	}
	kind, err := domain.ParseResultKind(req.Kind)
	if err != nil {
		httputil.BadRequest(w, "invalid kind")
		return userId, domain.ResultKindUnknown, req, false
	}
	if req.Title == "" {
		httputil.BadRequest(w, "title is required")
		return userId, kind, req, false
	}
	return userId, kind, req, true
}
