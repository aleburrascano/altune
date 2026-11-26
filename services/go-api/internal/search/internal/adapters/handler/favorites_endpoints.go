package handler

import (
	"altune/go-api/internal/search/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/httpserver"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser"
	"net/http"
)

type FavoriteDTO struct {
	Kind     string `json:"kind"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

type FavoriteRequest struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	ImageURL string `json:"image_url"`
}

func (h *DiscoveryHandler) handleListFavorites(w http.ResponseWriter, r *http.Request) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return
	}

	favorites, err := h.favoritesSvc.List(r.Context(), userId)
	if err != nil {
		httpserver.HandleServiceError(w, r, err)
		return
	}

	items := httpserver.MapSlice(favorites, func(f domain.Favorite) FavoriteDTO {
		return FavoriteDTO{
			Kind:     f.Kind.String(),
			Key:      f.Key,
			Title:    f.Title,
			Subtitle: f.Subtitle,
			ImageURL: f.ImageURL,
		}
	})

	httpserver.WriteJSON(w, http.StatusOK, httpserver.NewList(items))
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
		httpserver.HandleServiceError(w, r, err)
		return
	}

	httpserver.WriteJSON(w, http.StatusOK, FavoriteDTO{
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
		httpserver.HandleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func decodeFavorite(w http.ResponseWriter, r *http.Request) (ids.UserId, domain.ResultKind, FavoriteRequest, bool) {
	userId, ok := requestuser.RequireUserID(w, r)
	if !ok {
		return userId, domain.ResultKindUnknown, FavoriteRequest{}, false
	}

	var req FavoriteRequest
	if !httpserver.DecodeJSONWithCode(w, r, &req, apierror.CodeDiscoveryInvalidBody) {
		return userId, domain.ResultKindUnknown, req, false
	}
	kind, err := domain.ParseResultKind(req.Kind)
	if err != nil {
		httpserver.HandleServiceError(w, r, apierror.NewError(apierror.CodeDiscoveryInvalidKind, "invalid kind"))
		return userId, domain.ResultKindUnknown, req, false
	}
	if req.Title == "" {
		httpserver.HandleServiceError(w, r, invalidParam("title is required"))
		return userId, kind, req, false
	}
	return userId, kind, req, true
}
