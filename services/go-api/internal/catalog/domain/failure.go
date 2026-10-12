package domain

import "altune/go-api/internal/shared"

type FailureCode = shared.FailureCode

const (
	FailureNoMatchFound           = shared.FailureNoMatchFound
	FailureNoConfidentMatch       = shared.FailureNoConfidentMatch
	FailureSourceUnavailable      = shared.FailureSourceUnavailable
	FailureDownloadFailed         = shared.FailureDownloadFailed
	FailureStorageFailed          = shared.FailureStorageFailed
	FailureAcquisitionCancelled   = shared.FailureAcquisitionCancelled
	FailureAcquisitionFailed      = shared.FailureAcquisitionFailed
	FailureYtdlpError             = shared.FailureYtdlpError
	FailureAcquisitionInterrupted = shared.FailureAcquisitionInterrupted
	FailureAcquisitionRefused     = shared.FailureAcquisitionRefused
)

const FailureDetailSeparator = shared.FailureDetailSeparator

func JoinFailureReason(code FailureCode, detail string) string {
	return shared.JoinFailureReason(code, detail)
}

func SplitFailureReason(reason string) (FailureCode, string) {
	return shared.SplitFailureReason(reason)
}

func FailureMessage(reason *string) string {
	return shared.FailureMessage(reason)
}
