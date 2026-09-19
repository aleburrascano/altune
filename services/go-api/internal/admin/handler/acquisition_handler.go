package handler

import (
	acqPorts "altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"
)

// AcquisitionController reads the acquisition scheduler's status and drives its
// runtime kill switch. The live BackgroundAcquisitionScheduler satisfies it, so
// the admin surface can observe acquisition and Pause/Resume it at runtime
// without a redeploy. Pause refuses new jobs while in-flight work drains;
// Resume re-admits. Both are process-local and reset on restart.
type AcquisitionController interface {
	Status() acqPorts.AcquisitionStatus
	Pause()
	Resume()
}

// errAcquisitionUnavailable answers the pause/resume routes when no scheduler is
// wired (no audio store or enabled sources), matching the other kill switches'
// 503 for an unconfigured loop.
var errAcquisitionUnavailable = &codedError{
	msg:    "acquisition scheduler not configured",
	status: http.StatusServiceUnavailable,
	code:   "admin.acquisition_unavailable",
}

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

func (h *AdminHandler) serveAcquisition(w http.ResponseWriter, _ *http.Request) {
	if h.acquisition == nil {
		httputil.WriteJSON(w, http.StatusOK, newAcquisitionStatusDTO(acqPorts.AcquisitionStatus{
			ActiveJobs: []acqPorts.JobRecord{},
			Recent:     []acqPorts.JobRecord{},
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, newAcquisitionStatusDTO(h.acquisition.Status()))
}

func (h *AdminHandler) pauseAcquisition(w http.ResponseWriter, r *http.Request) {
	h.flipAcquisition(w, r, AcquisitionController.Pause)
}

func (h *AdminHandler) resumeAcquisition(w http.ResponseWriter, r *http.Request) {
	h.flipAcquisition(w, r, AcquisitionController.Resume)
}

// flipAcquisition applies one runtime kill-switch transition to the live
// acquisition scheduler and echoes the resulting status so the caller sees the
// new paused state, mirroring the alert/eval kill switches.
func (h *AdminHandler) flipAcquisition(w http.ResponseWriter, r *http.Request, flip func(AcquisitionController)) {
	if h.acquisition == nil {
		httputil.HandleServiceError(w, r, errAcquisitionUnavailable)
		return
	}
	flip(h.acquisition)
	st := h.acquisition.Status()
	auditKillSwitch(r.Context(), "acquisition", st.Paused)
	httputil.WriteJSON(w, http.StatusOK, newAcquisitionStatusDTO(st))
}
