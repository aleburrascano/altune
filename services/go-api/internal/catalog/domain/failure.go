package domain

import "strings"

// FailureCode is the stable, machine-readable prefix of a track's
// failure_reason. The acquisition side emits these codes; FailureMessage
// derives the user-facing failure_message from them. A persisted reason may
// carry a human-readable detail after the code, separated by
// FailureDetailSeparator.
type FailureCode string

const (
	FailureNoMatchFound         FailureCode = "no_match_found"
	FailureSourceUnavailable    FailureCode = "source_unavailable"
	FailureDownloadFailed       FailureCode = "download_failed"
	FailureStorageFailed        FailureCode = "storage_failed"
	FailureAcquisitionCancelled FailureCode = "acquisition_cancelled"
	FailureAcquisitionFailed    FailureCode = "acquisition_failed"
	FailureYtdlpError           FailureCode = "ytdlp_error"
	// FailureAcquisitionInterrupted marks a track whose acquisition job was lost
	// before completing (the process died mid-flight) and was swept from a stale
	// pending state to failed so the existing retry path can reclaim it.
	FailureAcquisitionInterrupted FailureCode = "acquisition_interrupted"
	// FailureAcquisitionRefused marks a track whose acquisition job was never
	// queued (the scheduler shed it under load or was shutting down), so it is
	// failed immediately and the retry path can reclaim it.
	FailureAcquisitionRefused FailureCode = "acquisition_refused"
)

// FailureDetailSeparator splits a failure_reason into its code and an optional
// human-readable detail (e.g. a candidate-rejection summary).
const FailureDetailSeparator = ": "

const genericFailureMessage = "Couldn't get this track"

var failureMessages = map[FailureCode]string{
	FailureNoMatchFound:           "Couldn't find this track",
	FailureSourceUnavailable:      "Couldn't reach the music source, try again",
	FailureDownloadFailed:         "Download failed",
	FailureStorageFailed:          "Couldn't save this track",
	FailureAcquisitionCancelled:   "Acquisition was cancelled",
	FailureAcquisitionFailed:      genericFailureMessage,
	FailureYtdlpError:             "Download error",
	FailureAcquisitionInterrupted: "Acquisition was interrupted",
	FailureAcquisitionRefused:     "Too busy to get this track, try again",
}

// Known reports whether c has an entry in the failure-message table.
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
