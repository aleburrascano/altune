package observeacquisition

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/observe"
)

type AcquisitionStatusSource interface {
	Status() acquisition.AcquisitionStatus
}

type acquisitionReader struct {
	source AcquisitionStatusSource
}

func NewAcquisitionReader(source AcquisitionStatusSource) observe.AcquisitionReader {
	return acquisitionReader{source: source}
}

func (r acquisitionReader) Status() observe.AcquisitionStatus {
	s := r.source.Status()
	return observe.AcquisitionStatus{
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

func observeTools(v acquisition.AcquisitionVerification) observe.AcquisitionTools {
	return observe.AcquisitionTools{Ffprobe: v.Ffprobe, Ffmpeg: v.Ffmpeg, Fpcalc: v.Fpcalc, YtDlp: v.YtDlp}
}

func observeJobs(jobs []acquisition.JobRecord) []observe.AcquisitionJob {
	out := make([]observe.AcquisitionJob, len(jobs))
	for i, j := range jobs {
		out[i] = observe.AcquisitionJob{
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
