package playback

import (
	persistence "altune/go-api/internal/playback/internal/adapters/persistence"
	domain "altune/go-api/internal/playback/internal/core/domain"
	ports "altune/go-api/internal/playback/internal/core/ports"
)

type (
	PostgresQueueStateRepository      = persistence.PostgresQueueStateRepository
	QueueStateDB                      = persistence.QueueStateDB
	PostgresDeletedIdentityRepository = persistence.PostgresDeletedIdentityRepository
)

const RepeatAll = domain.RepeatAll

var (
	ErrStaleQueueWrite       = ports.ErrStaleQueueWrite
	ErrQueueStateUnavailable = ports.ErrQueueStateUnavailable
	ErrQueuePositionMismatch = ports.ErrQueuePositionMismatch
	ErrCorruptStoredState    = ports.ErrCorruptStoredState
	ErrQueueStateNotFound    = ports.ErrQueueStateNotFound
)
