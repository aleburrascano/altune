package handler

import (
	"altune/go-api/internal/observe/adapters/acquisitionbridge"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

type acquisitionStatusDTO = acquisitionbridge.AcquisitionStatusDTO

func (h *Handler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, acquisitionbridge.ReadAcquisitionStatus(h.deps.Acquisition))
}
