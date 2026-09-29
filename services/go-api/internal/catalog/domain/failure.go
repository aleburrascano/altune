package domain

import "strings"

type FailureCode string

const (
	FailureNoMatchFound           FailureCode = "no_match_found"
	FailureNoConfidentMatch       FailureCode = "no_confident_match"
	FailureSourceUnavailable      FailureCode = "source_unavailable"
	FailureDownloadFailed         FailureCode = "download_failed"
	FailureStorageFailed          FailureCode = "storage_failed"
	FailureAcquisitionCancelled   FailureCode = "acquisition_cancelled"
	FailureAcquisitionFailed      FailureCode = "acquisition_failed"
	FailureYtdlpError             FailureCode = "ytdlp_error"
	FailureAcquisitionInterrupted FailureCode = "acquisition_interrupted"
	FailureAcquisitionRefused     FailureCode = "acquisition_refused"
)

const FailureDetailSeparator = ": "

const genericFailureMessage = "Couldn't get this track"

var failureMessages = map[FailureCode]string{
	FailureNoMatchFound:           "Couldn't find this track",
	FailureNoConfidentMatch:       "Couldn't find this track",
	FailureSourceUnavailable:      "Couldn't reach the music source, try again",
	FailureDownloadFailed:         "Download failed",
	FailureStorageFailed:          "Couldn't save this track",
	FailureAcquisitionCancelled:   "Acquisition was cancelled",
	FailureAcquisitionFailed:      genericFailureMessage,
	FailureYtdlpError:             "Download error",
	FailureAcquisitionInterrupted: "Acquisition was interrupted",
	FailureAcquisitionRefused:     "Too busy to get this track, try again",
}

func (c FailureCode) Known() bool {
	_, ok := failureMessages[c]
	return ok
}

func FailureMessage(reason *string) string {
	if reason == nil {
		return "Acquisition failed"
	}
	code, _, _ := strings.Cut(*reason, FailureDetailSeparator)
	if msg, ok := failureMessages[FailureCode(code)]; ok {
		return msg
	}
	return genericFailureMessage
}
