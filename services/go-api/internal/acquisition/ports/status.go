package ports

import "time"

// AcquisitionVerification reports which external acquisition tools are armed.
// Streamrip is true when the configured streamrip binary is runnable, or when no
// streamrip service is enabled (an opt-in source that is not wired cannot be
// degraded).
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

// JobRecord is a point-in-time snapshot of a single acquisition job.
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

// AcquisitionStatus is the observable state of the background acquisition
// scheduler, consumed by the admin surface.
//
// Rejected counts jobs the scheduler refused (queue full or shutting down).
// QueueDepth is the number of admission slots currently held (in-flight plus
// pending jobs) out of QueueCapacity; depth at capacity means new arrivals are
// being shed.
//
// Paused reports the runtime kill switch: true after Pause,
// when the scheduler refuses new jobs while in-flight work drains. It is
// process-local and resets on restart.
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
