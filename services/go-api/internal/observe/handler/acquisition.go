package handler

import (
	acqPorts "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"
)

type acquisitionVerificationDTO struct {
	Ffprobe   bool `json:"ffprobe"`
	Ffmpeg    bool `json:"ffmpeg"`
	Fpcalc    bool `json:"fpcalc"`
	YtDlp     bool `json:"yt_dlp"`
	Streamrip bool `json:"streamrip"`
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
	InFlight      int                        `json:"in_flight"`
	Succeeded     uint64                     `json:"succeeded"`
	Failed        uint64                     `json:"failed"`
	Rejected      uint64                     `json:"rejected"`
	Paused        bool                       `json:"paused"`
	QueueDepth    int                        `json:"queue_depth"`
	QueueCapacity int                        `json:"queue_capacity"`
	Verification  acquisitionVerificationDTO `json:"verification"`
	ActiveJobs    []jobRecordDTO             `json:"jobs"`
	Recent        []jobRecordDTO             `json:"recent"`
}

func newAcquisitionStatusDTO(s acqPorts.AcquisitionStatus) acquisitionStatusDTO {
	return acquisitionStatusDTO{
		InFlight:      s.InFlight,
		Succeeded:     s.Succeeded,
		Failed:        s.Failed,
		Rejected:      s.Rejected,
		Paused:        s.Paused,
		QueueDepth:    s.QueueDepth,
		QueueCapacity: s.QueueCapacity,
		Verification: acquisitionVerificationDTO{
			Ffprobe:   s.Verification.Ffprobe,
			Ffmpeg:    s.Verification.Ffmpeg,
			Fpcalc:    s.Verification.Fpcalc,
			YtDlp:     s.Verification.YtDlp,
			Streamrip: s.Verification.Streamrip,
		},
		ActiveJobs: newJobRecordDTOs(s.ActiveJobs),
		Recent:     newJobRecordDTOs(s.Recent),
	}
}

func newJobRecordDTOs(jobs []acqPorts.JobRecord) []jobRecordDTO {
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

func (h *Handler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Acquisition == nil {
		httputil.WriteJSON(w, http.StatusOK, newAcquisitionStatusDTO(acqPorts.AcquisitionStatus{
			ActiveJobs: []acqPorts.JobRecord{},
			Recent:     []acqPorts.JobRecord{},
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, newAcquisitionStatusDTO(h.deps.Acquisition.Status()))
}
