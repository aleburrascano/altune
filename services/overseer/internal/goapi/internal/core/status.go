package core

type Status int32

const (
	StatusConnecting Status = iota
	StatusUp
	StatusDown
)

func (s Status) String() string {
	switch s {
	case StatusUp:
		return "up"
	case StatusDown:
		return "down"
	case StatusConnecting:
		return "connecting"
	default:
		return "unknown"
	}
}

func (s Status) PanelState() string {
	switch s {
	case StatusUp:
		return "live"
	case StatusDown:
		return "source_down"
	case StatusConnecting:
		return "stale"
	default:
		return "stale"
	}
}

const reasonConnecting = "connecting"

func (s Status) PanelReason(failureReason string) string {
	switch s {
	case StatusUp:
		return ""
	case StatusConnecting:
		return reasonConnecting
	default:
		return failureReason
	}
}
