package domain

import (
	"altune/go-api/internal/shared/music"
)

type FailureCode = music.FailureCode

const (
	FailureNoMatchFound           = music.FailureNoMatchFound
	FailureNoConfidentMatch       = music.FailureNoConfidentMatch
	FailureSourceUnavailable      = music.FailureSourceUnavailable
	FailureDownloadFailed         = music.FailureDownloadFailed
	FailureStorageFailed          = music.FailureStorageFailed
	FailureAcquisitionCancelled   = music.FailureAcquisitionCancelled
	FailureAcquisitionFailed      = music.FailureAcquisitionFailed
	FailureYtdlpError             = music.FailureYtdlpError
	FailureAcquisitionInterrupted = music.FailureAcquisitionInterrupted
	FailureAcquisitionRefused     = music.FailureAcquisitionRefused
)

const FailureDetailSeparator = music.FailureDetailSeparator

func JoinFailureReason(code FailureCode, detail string) string {
	return music.JoinFailureReason(code, detail)
}

func SplitFailureReason(reason string) (FailureCode, string) {
	return music.SplitFailureReason(reason)
}

func FailureMessage(reason *string) string {
	return music.FailureMessage(reason)
}
