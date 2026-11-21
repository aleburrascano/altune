package handler

import (
	"altune/go-api/internal/observe/internal/core/ports"
	"net/url"
	"time"
)

type acquisitionToolsDTO struct {
	Ffprobe bool `json:"ffprobe"`
	Ffmpeg  bool `json:"ffmpeg"`
	Fpcalc  bool `json:"fpcalc"`
	YtDlp   bool `json:"yt_dlp"`
}

type acquisitionJobDTO struct {
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
	InFlight           int                 `json:"in_flight"`
	Succeeded          uint64              `json:"succeeded"`
	Failed             uint64              `json:"failed"`
	Rejected           uint64              `json:"rejected"`
	Paused             bool                `json:"paused"`
	QueueDepth         int                 `json:"queue_depth"`
	QueueCapacity      int                 `json:"queue_capacity"`
	OldestPendingAgeMs int64               `json:"oldest_pending_age_ms"`
	QueueDepthKnown    bool                `json:"queue_depth_known"`
	Verification       acquisitionToolsDTO `json:"verification"`
	ActiveJobs         []acquisitionJobDTO `json:"jobs"`
	Recent             []acquisitionJobDTO `json:"recent"`
}

func newAcquisitionStatusDTO(s ports.AcquisitionStatus) acquisitionStatusDTO {
	return acquisitionStatusDTO{
		InFlight:           s.InFlight,
		Succeeded:          s.Succeeded,
		Failed:             s.Failed,
		Rejected:           s.Rejected,
		Paused:             s.Paused,
		QueueDepth:         s.QueueDepth,
		QueueCapacity:      s.QueueCapacity,
		OldestPendingAgeMs: s.OldestPendingAge.Milliseconds(),
		QueueDepthKnown:    s.QueueDepthKnown,
		Verification:       acquisitionToolsDTO(s.Tools),
		ActiveJobs:         newAcquisitionJobDTOs(s.ActiveJobs),
		Recent:             newAcquisitionJobDTOs(s.Recent),
	}
}

func newAcquisitionJobDTOs(jobs []ports.AcquisitionJob) []acquisitionJobDTO {
	out := make([]acquisitionJobDTO, len(jobs))
	for i, j := range jobs {
		out[i] = acquisitionJobDTO{
			TrackID:        j.TrackID,
			Title:          j.Title,
			Artist:         j.Artist,
			Album:          j.Album,
			SourceURL:      redactSourceURL(j.SourceURL),
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

func redactSourceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}).String()
}

func readAcquisitionStatus(reader ports.AcquisitionReader) acquisitionStatusDTO {
	if reader == nil {
		return newAcquisitionStatusDTO(ports.AcquisitionStatus{})
	}
	return newAcquisitionStatusDTO(reader.Status())
}
