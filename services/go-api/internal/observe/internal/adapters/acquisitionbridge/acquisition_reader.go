package acquisitionbridge

import (
	"altune/go-api/internal/acquisition/ports"
	"net/url"
	"time"
)

type AcquisitionReader interface {
	Status() ports.AcquisitionStatus
}

type AcquisitionVerificationDTO struct {
	Ffprobe bool `json:"ffprobe"`
	Ffmpeg  bool `json:"ffmpeg"`
	Fpcalc  bool `json:"fpcalc"`
	YtDlp   bool `json:"yt_dlp"`
}

type JobRecordDTO struct {
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

type AcquisitionStatusDTO struct {
	InFlight           int                        `json:"in_flight"`
	Succeeded          uint64                     `json:"succeeded"`
	Failed             uint64                     `json:"failed"`
	Rejected           uint64                     `json:"rejected"`
	Paused             bool                       `json:"paused"`
	QueueDepth         int                        `json:"queue_depth"`
	QueueCapacity      int                        `json:"queue_capacity"`
	OldestPendingAgeMs int64                      `json:"oldest_pending_age_ms"`
	QueueDepthKnown    bool                       `json:"queue_depth_known"`
	Verification       AcquisitionVerificationDTO `json:"verification"`
	ActiveJobs         []JobRecordDTO             `json:"jobs"`
	Recent             []JobRecordDTO             `json:"recent"`
}

func newAcquisitionStatusDTO(s ports.AcquisitionStatus) AcquisitionStatusDTO {
	return AcquisitionStatusDTO{
		InFlight:           s.InFlight,
		Succeeded:          s.Succeeded,
		Failed:             s.Failed,
		Rejected:           s.Rejected,
		Paused:             s.Paused,
		QueueDepth:         s.QueueDepth,
		QueueCapacity:      s.Workers,
		OldestPendingAgeMs: s.OldestPendingAge.Milliseconds(),
		QueueDepthKnown:    s.QueueDepthKnown,
		Verification: AcquisitionVerificationDTO{
			Ffprobe: s.Verification.Ffprobe,
			Ffmpeg:  s.Verification.Ffmpeg,
			Fpcalc:  s.Verification.Fpcalc,
			YtDlp:   s.Verification.YtDlp,
		},
		ActiveJobs: newJobRecordDTOs(s.ActiveJobs),
		Recent:     newJobRecordDTOs(s.Recent),
	}
}

func newJobRecordDTOs(jobs []ports.JobRecord) []JobRecordDTO {
	out := make([]JobRecordDTO, len(jobs))
	for i, j := range jobs {
		out[i] = JobRecordDTO{
			TrackID:        j.TrackID,
			Title:          j.Title,
			Artist:         j.Artist,
			Album:          j.Album,
			SourceURL:      redactSourceURL(j.SourceURL),
			ResolvedSource: j.ResolvedSource,
			State:          string(j.State),
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

func ReadAcquisitionStatus(reader AcquisitionReader) AcquisitionStatusDTO {
	if reader == nil {
		return newAcquisitionStatusDTO(ports.AcquisitionStatus{
			ActiveJobs: []ports.JobRecord{},
			Recent:     []ports.JobRecord{},
		})
	}
	return newAcquisitionStatusDTO(reader.Status())
}
