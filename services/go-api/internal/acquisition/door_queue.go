package acquisition

import (
	persistence "altune/go-api/internal/acquisition/internal/adapters/persistence"
	domain "altune/go-api/internal/acquisition/internal/core/domain"
	ports "altune/go-api/internal/acquisition/internal/core/ports"
	service "altune/go-api/internal/acquisition/service"
)

type (
	AcquisitionOutcome       = ports.AcquisitionOutcome
	CandidateRejectionRecord = ports.CandidateRejectionRecord
	Job                      = ports.Job
	JobState                 = ports.JobState
	PgxJobQueue              = persistence.PgxJobQueue
	PgxRejectionStore        = persistence.PgxRejectionStore
	PgxUpgradeStore          = persistence.PgxUpgradeStore
	RejectionBasis           = ports.RejectionBasis
	StoredFile               = ports.StoredFile
)

const (
	CooldownReacquire = ports.CooldownReacquire
	CooldownRetry     = ports.CooldownRetry
	JobKindAcquire    = ports.JobKindAcquire
	JobKindReplace    = ports.JobKindReplace
	JobRunning        = ports.JobRunning
	JobSucceeded      = ports.JobSucceeded
	TrackFailed       = ports.TrackFailed
)

var (
	ErrJobKindConflict = domain.ErrJobKindConflict
	ErrNoJobAvailable  = domain.ErrNoJobAvailable
)

var WithPollInterval = service.WithPollInterval
