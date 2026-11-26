package ports

import "time"

type AcquisitionTools struct {
	Ffprobe bool
	Ffmpeg  bool
	Fpcalc  bool
	YtDlp   bool
}

type AcquisitionJob struct {
	TrackID        string
	Title          string
	Artist         string
	Album          string
	SourceURL      string
	ResolvedSource string
	State          string
	Stage          string
	ScheduledAt    time.Time
	ElapsedMs      int64
	Reason         string
	Provenance     string
}

type DownloadStatusReport struct {
	InFlight         int
	Succeeded        uint64
	Failed           uint64
	Rejected         uint64
	Paused           bool
	QueueDepth       int
	QueueCapacity    int
	OldestPendingAge time.Duration
	QueueDepthKnown  bool
	Tools            AcquisitionTools
	ActiveJobs       []AcquisitionJob
	Recent           []AcquisitionJob
}

type AcquisitionReader interface {
	Status() DownloadStatusReport
}
