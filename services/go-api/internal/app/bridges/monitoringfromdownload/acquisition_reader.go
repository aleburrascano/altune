package monitoringfromdownload

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/platform/monitoring"
	"context"
)

type DownloadStatusReportSource interface {
	Status(context.Context) download.Status
}

type acquisitionReader struct {
	source DownloadStatusReportSource
}

func NewAcquisitionReader(source DownloadStatusReportSource) monitoring.AcquisitionReader {
	return acquisitionReader{source: source}
}

func (r acquisitionReader) Status(ctx context.Context) monitoring.DownloadStatusReport {
	s := r.source.Status(ctx)
	return monitoring.DownloadStatusReport{
		InFlight:         s.InFlight,
		Succeeded:        s.Succeeded,
		Failed:           s.Failed,
		Rejected:         s.Rejected,
		Paused:           s.Paused,
		QueueDepth:       s.QueueDepth,
		QueueCapacity:    s.Workers,
		OldestPendingAge: s.OldestPendingAge,
		QueueDepthKnown:  s.QueueDepthKnown,
		Tools:            observeTools(s.Verification),
		ActiveJobs:       observeJobs(s.ActiveJobs),
		Recent:           observeJobs(s.Recent),
	}
}

func observeTools(v download.Verification) monitoring.AcquisitionTools {
	return monitoring.AcquisitionTools{Ffprobe: v.Ffprobe, Ffmpeg: v.Ffmpeg, Fpcalc: v.Fpcalc, YtDlp: v.YtDlp}
}

func observeJobs(jobs []download.JobRecord) []monitoring.AcquisitionJob {
	out := make([]monitoring.AcquisitionJob, len(jobs))
	for i, j := range jobs {
		out[i] = monitoring.AcquisitionJob{
			TrackID:        j.TrackID,
			Title:          j.Title,
			Artist:         j.Artist,
			Album:          j.Album,
			SourceURL:      j.SourceURL,
			ResolvedSource: j.ResolvedSource,
			State:          string(j.State),
			Stage:          j.Stage,
			ScheduledAt:    j.ScheduledAt,
			ElapsedMs:      j.ElapsedMs,
			Reason:         j.Reason,
			Provenance:     j.Provenance,
		}
	}
	return out
}
