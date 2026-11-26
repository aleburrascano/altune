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

type UpgradeState interface {
	State(ctx context.Context, trackID ids.TrackId) (UpgradeStateSnapshot, error)
	SettleAcquisition(ctx context.Context, trackID ids.TrackId, fence ClaimNumber, rejectedSourceKeys []string, resetEvidence bool) error
}

func MergeUpgradeEvidence(trackEvidence, state json.RawMessage) (json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if len(trackEvidence) > 0 {
		if err := json.Unmarshal(trackEvidence, &merged); err != nil {
			return nil, fmt.Errorf("read track evidence: %w", err)
		}
		if merged == nil {
			merged = map[string]json.RawMessage{}
		}
	}
	for _, key := range UpgradeEvidenceKeys {
		delete(merged, key)
	}
	if len(state) > 0 {
		overlay := map[string]json.RawMessage{}
		if err := json.Unmarshal(state, &overlay); err != nil {
			return nil, fmt.Errorf("read upgrade state: %w", err)
		}
		for key, value := range overlay {
			merged[key] = value
		}
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
