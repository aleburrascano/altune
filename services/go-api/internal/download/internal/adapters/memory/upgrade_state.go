package memory

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
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

func (s *UpgradeState) State(_ context.Context, trackID ids.TrackId) (ports.UpgradeStateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := ports.UpgradeStateSnapshot{RejectedSourceKeys: slices.Clone(s.rejected[trackID])}
	if evidence, ok := s.evidence[trackID]; ok {
		snapshot.Evidence = []byte(evidence)
	}
	return snapshot, nil
}

func (s *UpgradeState) SettleAcquisition(_ context.Context, trackID ids.TrackId, _ ports.ClaimNumber, rejectedSourceKeys []string, evidence ports.EvidenceFate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejected[trackID] = ports.AppendRejectedSourceKeys(s.rejected[trackID], rejectedSourceKeys)
	if evidence == ports.ResetEvidence {
		s.evidence[trackID] = resetEvidence
	}
	return nil
}
