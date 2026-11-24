package domain

import shareddomain "altune/go-api/internal/shared/domain"

var (
	codePositionMsNegative    = shareddomain.CodePlaybackPositionMsNegative
	codeQueueTooLong          = shareddomain.CodePlaybackQueueTooLong
	codeStringTooLong         = shareddomain.CodePlaybackStringTooLong
	codeStringContainsNul     = shareddomain.CodePlaybackStringContainsNul
	codeCurrentIdxOutOfRange  = shareddomain.CodePlaybackCurrentIdxOutOfRange
	codeCurrentTrackIdMissing = shareddomain.CodePlaybackCurrentTrackIDMissing
	codeUnknownRepeatMode     = shareddomain.CodePlaybackUnknownRepeatMode
	codeUnknownSourceKind     = shareddomain.CodePlaybackUnknownSourceKind
	codeUnrecognizedSourceId  = shareddomain.CodePlaybackUnrecognizedSourceID
	codeMalformedTrackId      = shareddomain.CodePlaybackMalformedTrackID
	codeNaturalOrderMismatch  = shareddomain.CodePlaybackNaturalOrderMismatch
)

func newValidationError(code shareddomain.Code, msg string) *shareddomain.CodedError {
	return shareddomain.NewError(code, msg)
}
