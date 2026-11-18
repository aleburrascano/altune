package acquisition

import (
	domain "altune/go-api/internal/acquisition/domain"
	persistence "altune/go-api/internal/acquisition/internal/adapters/persistence"
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
	TrackFailed       = ports.TrackFailed
)

var (
	ErrJobKindConflict = domain.ErrJobKindConflict
	ErrNoJobAvailable  = domain.ErrNoJobAvailable
)

var WithPollInterval = service.WithPollInterval
