package core

import (
	"encoding/json"
	"time"
)

type State string

const (
	StateLive       State = "live"
	StateStale      State = "stale"
	StateSourceDown State = "source_down"
)

type Severity string

const (
	SeverityOK       Severity = "ok"
	SeverityWarn     Severity = "warn"
	SeverityCritical Severity = "critical"
)

func (s Severity) Worse(other Severity) bool {
	return severityRank(s) > severityRank(other)
}

func severityRank(s Severity) int {
	switch s {
	case SeverityCritical:
		return 2
	case SeverityWarn:
		return 1
	default:
		return 0
	}
}

type Snapshot struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	State     State           `json:"state"`
	Severity  Severity        `json:"severity"`
	Headline  string          `json:"headline"`
	Reason    string          `json:"reason,omitempty"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Data      json.RawMessage `json:"data"`
	Spark     []SparkPoint    `json:"spark,omitempty"`
}

type SparkPoint struct {
	At time.Time `json:"at"`
	V  float64   `json:"v"`
}

func StaleState(unreachable, haveData bool) State {
	switch {
	case unreachable:
		return StateSourceDown
	case haveData:
		return StateLive
	default:
		return StateStale
	}
}

func MarshalData(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
