package handler

import (
	"net/http"

	"altune/go-api/internal/admin/providerhealth"
	"altune/go-api/internal/shared/httputil"
)

func (h *AdminHandler) serveProviders(w http.ResponseWriter, _ *http.Request) {
	if h.providerHealth == nil {
		httputil.WriteJSON(w, http.StatusOK, []providerhealth.ProviderSnapshot{})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.providerHealth.Snapshot())
}
