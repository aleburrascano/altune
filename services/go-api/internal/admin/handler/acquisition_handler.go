package handler

import (
	"net/http"

	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/shared/httputil"
)

// AcquisitionStatusReader is the read side of the acquisition pipeline panel,
// satisfied by the background scheduler. Defined here (where consumed) so the
// admin handler depends on a narrow interface, not the scheduler struct.
type AcquisitionStatusReader interface {
	Status() acqService.AcquisitionStatus
}

func (h *AdminHandler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	if h.acquisition == nil {
		httputil.WriteJSON(w, http.StatusOK, acqService.AcquisitionStatus{
			Jobs:        []acqService.JobRecord{},
			Recent:      []acqService.JobRecord{},
			RecentFails: []acqService.FailureRecord{},
		})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.acquisition.Status())
}
