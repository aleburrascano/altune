package handler

import (
	"net/http"
	"time"

	acqService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/shared/httputil"
)

type AcquisitionStatusReader interface {
	Status() acqService.AcquisitionStatus
}

type acquisitionVerificationDTO struct {
	Ffprobe bool `json:"ffprobe"`
	Ffmpeg  bool `json:"ffmpeg"`
	Fpcalc  bool `json:"fpcalc"`
}

type jobRecordDTO struct {
	TrackID        string    `json:"track_id"`
	Title          string    `json:"title,omitempty"`
	Artist         string    `json:"artist,omitempty"`
	Album          string    `json:"album,omitempty"`
	SourceURL      string    `json:"source_url,omitempty"`
	ResolvedSource string    `json:"source,omitempty"`
	State          string    `json:"state"`
	Stage          string    `json:"stage,omitempty"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	ElapsedMs      int64     `json:"elapsed_ms"`
	Reason         string    `json:"reason,omitempty"`
	Provenance     string    `json:"provenance,omitempty"`
}

type acquisitionStatusDTO struct {
	InFlight     int                        `json:"in_flight"`
	Succeeded    uint64                     `json:"succeeded"`
	Failed       uint64                     `json:"failed"`
	Verification acquisitionVerificationDTO `json:"verification"`
	ActiveJobs   []jobRecordDTO             `json:"jobs"`
	Recent       []jobRecordDTO             `json:"recent"`
}

func newAcquisitionStatusDTO(s acqService.AcquisitionStatus) acquisitionStatusDTO {
	return acquisitionStatusDTO{
		InFlight:  s.InFlight,
		Succeeded: s.Succeeded,
		Failed:    s.Failed,
		Verification: acquisitionVerificationDTO{
			Ffprobe: s.Verification.Ffprobe,
			Ffmpeg:  s.Verification.Ffmpeg,
			Fpcalc:  s.Verification.Fpcalc,
		},
		ActiveJobs: newJobRecordDTOs(s.ActiveJobs),
		Recent:     newJobRecordDTOs(s.Recent),
	}
}

func newJobRecordDTOs(jobs []acqService.JobRecord) []jobRecordDTO {
	out := make([]jobRecordDTO, len(jobs))
	for i, j := range jobs {
		out[i] = jobRecordDTO{
			TrackID:        j.TrackID,
			Title:          j.Title,
			Artist:         j.Artist,
			Album:          j.Album,
			SourceURL:      j.SourceURL,
			ResolvedSource: j.ResolvedSource,
			State:          j.State,
			Stage:          j.Stage,
			ScheduledAt:    j.ScheduledAt.UTC(),
			ElapsedMs:      j.ElapsedMs,
			Reason:         j.Reason,
			Provenance:     j.Provenance,
		}
	}
	return out
}

func (h *AdminHandler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	if h.acquisition == nil {
		httputil.WriteJSON(w, http.StatusOK, newAcquisitionStatusDTO(acqService.AcquisitionStatus{
			ActiveJobs: []acqService.JobRecord{},
			Recent:     []acqService.JobRecord{},
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, newAcquisitionStatusDTO(h.acquisition.Status()))
}
