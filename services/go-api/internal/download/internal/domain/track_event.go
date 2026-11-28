package domain

type TrackEvent int

const (
	EventMarkReady TrackEvent = iota
	EventReplaceAudio
	EventMarkFailed
	EventFailPending
	EventRevertToPending
)

func TrackEvents() []TrackEvent {
	return []TrackEvent{EventMarkReady, EventReplaceAudio, EventMarkFailed, EventFailPending, EventRevertToPending}
}
