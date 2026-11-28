package ports

import (
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type UpgradeCandidate struct {
	TrackID ids.TrackId
	UserID  ids.UserId
	Grade   string
}

type UpgradeSelection struct {
	Limit      int
	Generation int
	RetryAfter time.Duration
	MaxQueued  int

	CheckInPlace bool

	LowBitrateFloorKbps int

	Upgradable []TrackStatus
}

type UpgradeAttempt struct {
	RejectedSourceKey string
	CountsAsTry       bool
}

type InPlaceRecord struct {
	AudioRef           string
	Generation         int
	Verdict            string
	Basis              string
	Score              float64
	BitErrorRate       float64
	Grade              string
	UserUploadMismatch bool
	MayLower           bool
}

type InPlaceStore interface {
	RecordInPlace(ctx context.Context, trackID ids.TrackId, record InPlaceRecord) (recorded bool, err error)
	NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (failures int, err error)
}

type UpgradeStore interface {
	Candidates(ctx context.Context, selection UpgradeSelection) ([]UpgradeCandidate, error)
	NoteAttempt(ctx context.Context, trackID ids.TrackId, attempt UpgradeAttempt) (tries int, err error)
	GiveUp(ctx context.Context, trackID ids.TrackId, generation int, reason string) error
}

var UpgradeEvidenceKeys = []string{
	"upgrade_tried_at", "upgrade_tries",
	"best_available_generation", "best_available_reason", "best_available_at",
	"in_place_generation", "in_place_at", "in_place_basis", "in_place_bit_error_rate", "in_place_failures",
}

const MaxRejectedSourceKeys = 25

type UpgradeStateSnapshot struct {
	Evidence           json.RawMessage
	RejectedSourceKeys []string
}

type EvidenceFate int

const (
	KeepEvidence EvidenceFate = iota
	ResetEvidence
)

type UpgradeState interface {
	State(ctx context.Context, trackID ids.TrackId) (UpgradeStateSnapshot, error)
	SettleAcquisition(ctx context.Context, trackID ids.TrackId, fence ClaimNumber, rejectedSourceKeys []string, evidence EvidenceFate) error
}

func decodeJSONObject(raw json.RawMessage, what string) (map[string]json.RawMessage, error) {
	object := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return object, nil
	}
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, fmt.Errorf("read %s: %w", what, err)
	}
	if object == nil {
		object = map[string]json.RawMessage{}
	}
	return object, nil
}

func MergeUpgradeEvidence(trackEvidence, state json.RawMessage) (json.RawMessage, error) {
	merged, err := decodeJSONObject(trackEvidence, "track evidence")
	if err != nil {
		return nil, err
	}
	for _, key := range UpgradeEvidenceKeys {
		delete(merged, key)
	}
	overlay, err := decodeJSONObject(state, "upgrade state")
	if err != nil {
		return nil, err
	}
	for key, value := range overlay {
		merged[key] = value
	}
	if len(merged) == 0 && len(trackEvidence) == 0 {
		return trackEvidence, nil
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("write merged evidence: %w", err)
	}
	return out, nil
}

func AppendRejectedSourceKeys(existing, added []string) []string {
	out := append([]string(nil), existing...)
	for _, key := range added {
		if key == "" || containsKey(out, key) {
			continue
		}
		out = append(out, key)
		if len(out) > MaxRejectedSourceKeys {
			out = out[len(out)-MaxRejectedSourceKeys:]
		}
	}
	return out
}

func containsKey(keys []string, key string) bool {
	for _, existing := range keys {
		if existing == key {
			return true
		}
	}
	return false
}
