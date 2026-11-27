package ports

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

func TrackStatuses() []TrackStatus { return []TrackStatus{TrackPending, TrackReady, TrackFailed} }

func (s TrackStatus) Wire() string {
	switch s {
	case TrackPending:
		return "pending"
	case TrackReady:
		return "ready"
	case TrackFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type StatusRules interface {
	Next(from TrackStatus, event TrackEvent) (TrackStatus, error)
	Claimable() []TrackStatus
	Retryable() []TrackStatus
	Upgradable() []TrackStatus
}

func StatusWires(statuses []TrackStatus) []string {
	wires := make([]string, len(statuses))
	for i, s := range statuses {
		wires[i] = s.Wire()
	}
	return wires
}
