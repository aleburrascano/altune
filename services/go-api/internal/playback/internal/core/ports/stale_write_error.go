package ports

import shareddomain "altune/go-api/internal/shared/domain"

var ErrStaleQueueWrite error = shareddomain.NewError(shareddomain.CodePlaybackStaleQueueWrite,
	"queue state not saved: a newer snapshot is already stored")

var ErrQueuePositionMismatch error = shareddomain.NewError(shareddomain.CodePlaybackQueuePositionMismatch,
	"queue position not saved: the stored queue does not hold that track at that index")
