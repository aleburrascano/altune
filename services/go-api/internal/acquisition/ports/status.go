package ports

import "time"

type AcquisitionVerification struct {
	Ffprobe   bool
	Ffmpeg    bool
	Fpcalc    bool
	YtDlp     bool
	Streamrip bool
}

func (v AcquisitionVerification) FullyArmed() bool {
	return v.Ffprobe && v.Ffmpeg && v.Fpcalc && v.YtDlp && v.Streamrip
}

type JobRecord struct {
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

type AcquisitionStatus struct {
	InFlight      int
	Succeeded     uint64
	Failed        uint64
	Rejected      uint64
	Paused        bool
	QueueDepth    int
	QueueCapacity int
	Verification  AcquisitionVerification
	ActiveJobs    []JobRecord
	Recent        []JobRecord
}
