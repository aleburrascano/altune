package handler

import (
	"net/http"

	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/shared/httputil"
)

type AcquisitionStatusReader interface {
	Status() acqService.AcquisitionStatus
}

func (h *AdminHandler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	if h.acquisition == nil {
		httputil.WriteJSON(w, http.StatusOK, acqService.AcquisitionStatus{
			ActiveJobs: []acqService.JobRecord{},
			Recent:     []acqService.JobRecord{},
		})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.acquisition.Status())
}
