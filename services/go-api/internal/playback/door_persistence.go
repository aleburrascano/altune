package playback

import (
	persistence "altune/go-api/internal/playback/internal/adapters/persistence"
	domain "altune/go-api/internal/playback/internal/core/domain"
	ports "altune/go-api/internal/playback/internal/core/ports"
)

type (
	PgxQueueStateRepository      = persistence.PgxQueueStateRepository
	QueueStateDB                 = persistence.QueueStateDB
	PgxDeletedIdentityRepository = persistence.PgxDeletedIdentityRepository
)

const RepeatAll = domain.RepeatAll

var (
	ErrStaleQueueWrite       = ports.ErrStaleQueueWrite
	ErrQueueStateUnavailable = ports.ErrQueueStateUnavailable
	ErrQueuePositionMismatch = ports.ErrQueuePositionMismatch
	ErrCorruptStoredState    = ports.ErrCorruptStoredState
	ErrQueueStateNotFound    = ports.ErrQueueStateNotFound
)
