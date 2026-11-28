package handler

import (
	"altune/go-api/internal/platform/monitoring/internal/ports"
	"context"
	"net/url"
)

func newAcquisitionStatusDTO(s ports.DownloadStatusReport) acquisitionStatusDTO {
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

func readAcquisitionStatus(ctx context.Context, reader ports.AcquisitionReader) acquisitionStatusDTO {
	if reader == nil {
		return newAcquisitionStatusDTO(ports.DownloadStatusReport{})
	}
	return newAcquisitionStatusDTO(reader.Status(ctx))
}
