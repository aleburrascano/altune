package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"slices"
	"sync"
)

const resetEvidence = "{}"

type UpgradeState struct {
	mu       sync.Mutex
	evidence map[ids.TrackId]string
	rejected map[ids.TrackId][]string
}

var _ ports.UpgradeState = (*UpgradeState)(nil)

func NewUpgradeState() *UpgradeState {
	return &UpgradeState{evidence: map[ids.TrackId]string{}, rejected: map[ids.TrackId][]string{}}
}

func (s *UpgradeState) SeedEvidence(trackID ids.TrackId, evidence json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evidence[trackID] = string(evidence)
}

func (s *UpgradeState) State(_ context.Context, trackID ids.TrackId) (domain.UpgradeStateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := domain.UpgradeStateSnapshot{RejectedSourceKeys: slices.Clone(s.rejected[trackID])}
	if evidence, ok := s.evidence[trackID]; ok {
		snapshot.Evidence = []byte(evidence)
	}
	return snapshot, nil
}

func (s *UpgradeState) SettleAcquisition(_ context.Context, trackID ids.TrackId, _ domain.ClaimNumber, rejectedSourceKeys []string, evidence domain.EvidenceFate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejected[trackID] = domain.AppendRejectedSourceKeys(s.rejected[trackID], rejectedSourceKeys)
	if evidence == domain.ResetEvidence {
		s.evidence[trackID] = resetEvidence
	}
	return nil
}
