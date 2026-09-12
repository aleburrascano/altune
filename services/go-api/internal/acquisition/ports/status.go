package ports

import "time"

// AcquisitionVerification reports which external acquisition tools are armed.
type AcquisitionVerification struct {
	Ffprobe bool
	Ffmpeg  bool
	Fpcalc  bool
	YtDlp   bool
}

func (v AcquisitionVerification) FullyArmed() bool {
	return v.Ffprobe && v.Ffmpeg && v.Fpcalc && v.YtDlp
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
type AcquisitionStatus struct {
	InFlight     int
	Succeeded    uint64
	Failed       uint64
	Rejected     uint64
	Verification AcquisitionVerification
	ActiveJobs   []JobRecord
	Recent       []JobRecord
}
