package playback

import (
	domain "altune/go-api/internal/playback/domain"
	persistence "altune/go-api/internal/playback/internal/adapters/persistence"
	ports "altune/go-api/internal/playback/internal/core/ports"
)

type (
	PgxQueueStateRepository      = persistence.PgxQueueStateRepository
	QueueStateDB                 = persistence.QueueStateDB
	PgxDeletedIdentityRepository = persistence.PgxDeletedIdentityRepository
)

const RepeatAll = domain.RepeatAll

var (
	ErrStaleQueueWrite       = domain.ErrStaleQueueWrite
	ErrQueueStateUnavailable = domain.ErrQueueStateUnavailable
	ErrQueuePositionMismatch = domain.ErrQueuePositionMismatch
	ErrCorruptStoredState    = ports.ErrCorruptStoredState
)
