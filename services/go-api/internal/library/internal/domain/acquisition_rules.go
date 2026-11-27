package domain

type AcquisitionEvent int

const (
	EventMarkReady AcquisitionEvent = iota
	EventReplaceAudio
	EventMarkFailed
	EventFailPending
	EventRevertToPending
)

var acquisitionEvents = []AcquisitionEvent{
	EventMarkReady, EventReplaceAudio, EventMarkFailed, EventFailPending, EventRevertToPending,
}

func AcquisitionEvents() []AcquisitionEvent {
	return append([]AcquisitionEvent(nil), acquisitionEvents...)
}

func AcquisitionStatuses() []DownloadStatus {
	return []DownloadStatus{AcquisitionPending, AcquisitionReady, AcquisitionFailed}
}

func (e AcquisitionEvent) String() string {
	switch e {
	case EventMarkReady:
		return "mark ready"
	case EventReplaceAudio:
		return "replace audio"
	case EventMarkFailed:
		return "mark failed"
	case EventFailPending:
		return "fail acquisition"
	case EventRevertToPending:
		return "revert to pending"
	default:
		return "unknown event"
	}
}

func Transition(from DownloadStatus, event AcquisitionEvent) (DownloadStatus, error) {
	switch event {
	case EventMarkReady:
		return AcquisitionReady, nil
	case EventReplaceAudio:
		if from == AcquisitionReady {
			return AcquisitionReady, nil
		}
	case EventMarkFailed:
		if from != AcquisitionFailed {
			return AcquisitionFailed, nil
		}
	case EventFailPending:
		if from == AcquisitionPending {
			return AcquisitionFailed, nil
		}
	case EventRevertToPending:
		if from != AcquisitionPending {
			return AcquisitionPending, nil
		}
	}
	return from, illegalTransition(event.String(), from)
}

func ClaimableStatuses() []DownloadStatus { return []DownloadStatus{AcquisitionPending} }

func RetryableStatuses() []DownloadStatus { return []DownloadStatus{AcquisitionFailed} }

func UpgradableStatuses() []DownloadStatus { return []DownloadStatus{AcquisitionReady} }
