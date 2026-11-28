package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"
)

type SettleCall struct {
	Fence domain.ClaimNumber
	Keys  []string
	Reset bool
}

type upgradeRow struct {
	evidence map[string]json.RawMessage
	rejected []string
}

type UpgradeState struct {
	mu            sync.Mutex
	now           clock.Func
	rows          map[ids.TrackId]*upgradeRow
	err           error
	attempts      []domain.UpgradeAttempt
	gaveUp        []string
	settled       []SettleCall
	inPlaceWrites int
	picks         []domain.UpgradeCandidate
	pickErr       error
	selection     domain.UpgradeSelection
}

var (
	_ ports.UpgradeState = (*UpgradeState)(nil)
	_ ports.UpgradeStore = (*UpgradeState)(nil)
	_ ports.InPlaceState = (*UpgradeState)(nil)
)

func NewUpgradeState(now clock.Func) *UpgradeState {
	return &UpgradeState{now: now, rows: map[ids.TrackId]*upgradeRow{}}
}

func (s *UpgradeState) FailWith(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func (s *UpgradeState) Seed(trackID ids.TrackId, snapshot domain.UpgradeStateSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row := &upgradeRow{rejected: slices.Clone(snapshot.RejectedSourceKeys)}
	if len(snapshot.Evidence) > 0 {
		if err := json.Unmarshal(snapshot.Evidence, &row.evidence); err != nil {
			panic(fmt.Sprintf("memory.UpgradeState.Seed: evidence %s is not a JSON object: %v", snapshot.Evidence, err))
		}
	}
	s.rows[trackID] = row
}

func (s *UpgradeState) PickCandidates(picks []domain.UpgradeCandidate, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.picks, s.pickErr = picks, err
}

func (s *UpgradeState) LastSelection() domain.UpgradeSelection {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selection
}

func (s *UpgradeState) Attempts() []domain.UpgradeAttempt {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.attempts)
}

func (s *UpgradeState) GaveUp() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.gaveUp)
}

func (s *UpgradeState) Settled() []SettleCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.settled)
}

func (s *UpgradeState) InPlaceStateWrites() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inPlaceWrites
}

func (s *UpgradeState) row(trackID ids.TrackId) *upgradeRow {
	r, ok := s.rows[trackID]
	if !ok {
		r = &upgradeRow{}
		s.rows[trackID] = r
	}
	return r
}

func (r *upgradeRow) set(key string, value any) {
	if r.evidence == nil {
		r.evidence = map[string]json.RawMessage{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("memory.UpgradeState: marshal %s: %v", key, err))
	}
	r.evidence[key] = raw
}

func (r *upgradeRow) count(key string) int {
	var n int
	if raw, ok := r.evidence[key]; ok {
		_ = json.Unmarshal(raw, &n)
	}
	return n
}

func (s *UpgradeState) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

func (s *UpgradeState) Candidates(_ context.Context, selection domain.UpgradeSelection) ([]domain.UpgradeCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selection = selection
	return slices.Clone(s.picks), s.pickErr
}

func (s *UpgradeState) NoteAttempt(_ context.Context, trackID ids.TrackId, attempt domain.UpgradeAttempt) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, attempt)
	if s.err != nil {
		return 0, s.err
	}
	r := s.row(trackID)
	tries := r.count("upgrade_tries")
	if attempt.CountsAsTry {
		tries++
	}
	r.set("upgrade_tried_at", s.stamp())
	r.set("upgrade_tries", tries)
	if attempt.RejectedSourceKey != "" {
		r.rejected = domain.AppendRejectedSourceKeys(r.rejected, []string{attempt.RejectedSourceKey})
	}
	return tries, nil
}

func (s *UpgradeState) GiveUp(_ context.Context, trackID ids.TrackId, generation int, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gaveUp = append(s.gaveUp, reason)
	if s.err != nil {
		return s.err
	}
	r := s.row(trackID)
	r.set("best_available_generation", generation)
	r.set("best_available_reason", reason)
	r.set("best_available_at", s.stamp())
	r.set("upgrade_tries", 0)
	return nil
}

func (s *UpgradeState) RecordInPlaceState(_ context.Context, trackID ids.TrackId, generation int, basis string, bitErrorRate float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inPlaceWrites++
	if s.err != nil {
		return s.err
	}
	r := s.row(trackID)
	delete(r.evidence, "in_place_failures")
	r.set("in_place_generation", generation)
	r.set("in_place_at", s.stamp())
	if basis != "" {
		r.set("in_place_basis", basis)
	}
	if bitErrorRate != 0 {
		r.set("in_place_bit_error_rate", bitErrorRate)
	}
	return nil
}

func (s *UpgradeState) NoteInPlaceFailure(_ context.Context, trackID ids.TrackId) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return 0, s.err
	}
	r := s.row(trackID)
	failures := r.count("in_place_failures") + 1
	r.set("upgrade_tried_at", s.stamp())
	r.set("in_place_failures", failures)
	return failures, nil
}

func (s *UpgradeState) State(_ context.Context, trackID ids.TrackId) (domain.UpgradeStateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return domain.UpgradeStateSnapshot{}, s.err
	}
	r, ok := s.rows[trackID]
	if !ok {
		return domain.UpgradeStateSnapshot{}, nil
	}
	snapshot := domain.UpgradeStateSnapshot{RejectedSourceKeys: slices.Clone(r.rejected)}
	if r.evidence != nil {
		raw, err := json.Marshal(maps.Clone(r.evidence))
		if err != nil {
			return domain.UpgradeStateSnapshot{}, fmt.Errorf("marshal upgrade evidence: %w", err)
		}
		snapshot.Evidence = raw
	}
	return snapshot, nil
}

func (s *UpgradeState) SettleAcquisition(_ context.Context, trackID ids.TrackId, fence domain.ClaimNumber, rejectedSourceKeys []string, evidence domain.EvidenceFate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settled = append(s.settled, SettleCall{Fence: fence, Keys: slices.Clone(rejectedSourceKeys), Reset: evidence == domain.ResetEvidence})
	if s.err != nil {
		return s.err
	}
	r := s.row(trackID)
	r.rejected = domain.AppendRejectedSourceKeys(r.rejected, rejectedSourceKeys)
	if evidence == domain.ResetEvidence {
		r.evidence = map[string]json.RawMessage{}
	}
	return nil
}
