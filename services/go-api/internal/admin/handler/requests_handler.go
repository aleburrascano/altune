package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/shared/httputil"
)

func (h *AdminHandler) serveRequests(w http.ResponseWriter, _ *http.Request) {
	if h.requests == nil {
		httputil.WriteJSON(w, http.StatusOK, []requeststore.RequestRecord{})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.requests.Snapshot())
}

func (h *AdminHandler) serveRequestDetail(w http.ResponseWriter, r *http.Request) {
	if h.requests == nil {
		httputil.HandleServiceError(w, r, errRequestNotFound)
		return
	}
	rec, ok := h.requests.Get(chi.URLParam(r, "corrID"))
	if !ok {
		httputil.HandleServiceError(w, r, errRequestNotFound)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, rec)
}
