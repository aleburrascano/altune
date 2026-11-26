package ports

import apierror "altune/go-api/internal/shared/apierror"

var ErrStaleQueueWrite error = apierror.NewError(apierror.CodePlaybackStaleQueueWrite,
	"queue state not saved: a newer snapshot is already stored")

var ErrQueuePositionMismatch error = apierror.NewError(apierror.CodePlaybackQueuePositionMismatch,
	"queue position not saved: the stored queue does not hold that track at that index")
