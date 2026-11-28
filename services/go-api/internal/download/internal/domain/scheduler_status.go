package domain

import "time"

type SchedulerStatus struct {
	InFlight         int
	Pending          int
	Succeeded        uint64
	Failed           uint64
	Rejected         uint64
	VerifySkipped    uint64
	Paused           bool
	QueueDepth       int
	OldestPendingAge time.Duration
	QueueDepthKnown  bool
	Workers          int
	Verification     Verification
	ActiveJobs       []JobRecord
	Recent           []JobRecord
}
