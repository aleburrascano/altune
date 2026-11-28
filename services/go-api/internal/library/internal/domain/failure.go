package domain

import (
	"altune/go-api/internal/shared/downloadfailure"
)

type FailureCode = downloadfailure.FailureCode

const (
	FailureNoMatchFound           = downloadfailure.FailureNoMatchFound
	FailureNoConfidentMatch       = downloadfailure.FailureNoConfidentMatch
	FailureSourceUnavailable      = downloadfailure.FailureSourceUnavailable
	FailureDownloadFailed         = downloadfailure.FailureDownloadFailed
	FailureStorageFailed          = downloadfailure.FailureStorageFailed
	FailureAcquisitionCancelled   = downloadfailure.FailureAcquisitionCancelled
	FailureAcquisitionFailed      = downloadfailure.FailureAcquisitionFailed
	FailureYtdlpError             = downloadfailure.FailureYtdlpError
	FailureAcquisitionInterrupted = downloadfailure.FailureAcquisitionInterrupted
	FailureAcquisitionRefused     = downloadfailure.FailureAcquisitionRefused
)

const FailureDetailSeparator = downloadfailure.FailureDetailSeparator

func JoinFailureReason(code FailureCode, detail string) string {
	return downloadfailure.JoinFailureReason(code, detail)
}

func SplitFailureReason(reason string) (FailureCode, string) {
	return downloadfailure.SplitFailureReason(reason)
}

func FailureMessage(reason *string) string {
	return downloadfailure.FailureMessage(reason)
}
