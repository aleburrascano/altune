package domain

type TrackStatus int

const (
	TrackPending TrackStatus = iota
	TrackReady
	TrackFailed
)

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

func StatusWires(statuses []TrackStatus) []string {
	wires := make([]string, len(statuses))
	for i, s := range statuses {
		wires[i] = s.Wire()
	}
	return wires
}
