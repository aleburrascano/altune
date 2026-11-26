package domain

import apierror "altune/go-api/internal/shared/apierror"

var (
	codePositionMsNegative    = apierror.CodePlaybackPositionMsNegative
	codeQueueTooLong          = apierror.CodePlaybackQueueTooLong
	codeStringTooLong         = apierror.CodePlaybackStringTooLong
	codeStringContainsNul     = apierror.CodePlaybackStringContainsNul
	codeCurrentIdxOutOfRange  = apierror.CodePlaybackCurrentIdxOutOfRange
	codeCurrentTrackIdMissing = apierror.CodePlaybackCurrentTrackIDMissing
	codeUnknownRepeatMode     = apierror.CodePlaybackUnknownRepeatMode
	codeUnknownSourceKind     = apierror.CodePlaybackUnknownSourceKind
	codeUnrecognizedSourceId  = apierror.CodePlaybackUnrecognizedSourceID
	codeMalformedTrackId      = apierror.CodePlaybackMalformedTrackID
	codeNaturalOrderMismatch  = apierror.CodePlaybackNaturalOrderMismatch
)

func newValidationError(code apierror.Code, msg string) *apierror.CodedError {
	return apierror.NewError(code, msg)
}
