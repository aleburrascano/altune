package ports

import (
	"context"
	"time"
)

type AcquisitionVerification struct {
	Ffprobe             bool
	Ffmpeg              bool
	Fpcalc              bool
	YtDlp               bool
	FingerprintVerified bool
}

type JobState string

const (
	JobQueued    JobState = "queued"
	JobRunning   JobState = "running"
	JobSucceeded JobState = "succeeded"
	JobFailed    JobState = "failed"
	JobCancelled JobState = "cancelled"
)

type VerifySkipRecorder interface {
	RecordVerifySkip(gate string)
}

const (
	SkipDecodeTimeout      = "decode_timeout"
	SkipDecoderUnavailable = "decoder_unavailable"
	SkipIdentifyFailed     = "identify_failed"
	SkipPreviewFallback    = "preview_fallback"
	SkipProbeFailed        = "probe_failed"
	SkipNoDuration         = "no_duration"
)

func (v AcquisitionVerification) FullyArmed() bool {
	return v.Ffprobe && v.Ffmpeg && v.Fpcalc && v.YtDlp
}

type JobRecord struct {
	TrackID        string
	Title          string
	Artist         string
	Album          string
	SourceURL      string
	ResolvedSource string
	State          JobState
	Stage          string
	ScheduledAt    time.Time
	ElapsedMs      int64
	Reason         string
	Provenance     string
	StageMs        map[string]int64
	Source         SourceFormat
	Stored         StoredFile
}

type AcquisitionStatus struct {
	InFlight         int
	Succeeded        uint64
	Failed           uint64
	Rejected         uint64
	VerifySkipped    uint64
	Paused           bool
	QueueDepth       int
	OldestPendingAge time.Duration
	QueueDepthKnown  bool
	QueueCapacity    int
	Verification     AcquisitionVerification
	ActiveJobs       []JobRecord
	Recent           []JobRecord
}

type AcquisitionOutcome struct {
	TrackID    string
	Outcome    JobState
	Reason     string
	ElapsedMs  int64
	StageMs    map[string]int64
	SourceHost string
	Source     SourceFormat
	Stored     StoredFile
}

type StoredFile struct {
	Bytes int64
	Codec string
}

type OutcomeRecorder interface {
	Record(ctx context.Context, o AcquisitionOutcome) error
}
