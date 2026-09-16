package core

import (
	"encoding/json"
	"time"
)

// State is the first-class rendering state every panel reports. It replaces the
// old "stale bool + consumer Status" split with one enum, so the frontend renders
// the three states uniformly. Today a bool carried staleness and source-down came
// from a consumer's typed Status; a single enum is the whole story now.
type State string

const (
	// StateLive means the last collect succeeded and the source is reachable.
	StateLive State = "live"
	// StateStale means data is held but its freshness is uncertain — a source
	// still connecting, a first poll pending, or one of several sources degraded.
	StateStale State = "stale"
	// StateSourceDown means the watched app is unreachable; the panel shows its
	// last-known state (never blank) flagged source-down.
	StateSourceDown State = "source_down"
)

// Snapshot is a bucket's contribution to the JSON API: an envelope carrying the
// bucket's identity, its rendering state, when its data was last fresh, and a
// bucket-specific JSON payload the bucket marshals itself. The frontend's panel
// registry keys on ID; a bucket with no bespoke panel renders through a generic
// fallback that reads State + Data.
type Snapshot struct {
	ID        string          `json:"id"`
	Title     string          `json:"title"`
	State     State           `json:"state"`
	UpdatedAt time.Time       `json:"updatedAt"`
	Data      json.RawMessage `json:"data"`
}

// StaleState maps a read-backed bucket's "currently unreachable" flag and whether
// it has ever mirrored data onto the panel state. It is the one place the several
// read buckets share the mapping: an unreachable source is source_down (with the
// last-known data preserved), a bucket that has never once succeeded is stale, and
// a fresh reachable read is live.
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

// MarshalData is the helper buckets use to build a Snapshot from a typed payload.
// It marshals v into Data; on the (practically impossible) marshal failure it
// substitutes an empty JSON object so a snapshot is always well-formed JSON and a
// single bad payload degrades rather than breaks the API response. Buckets that
// need to distinguish the failure can marshal themselves and set Data directly.
func MarshalData(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
