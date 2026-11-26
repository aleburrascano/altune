package upgradestatetest

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/ids"
	"context"
	"slices"
	"sync"
)

type State struct {
	mu       sync.Mutex
	evidence map[ids.TrackId]string
	rejected map[ids.TrackId][]string
}

var _ download.UpgradeState = (*State)(nil)

func New() *State {
	return &State{evidence: map[ids.TrackId]string{}, rejected: map[ids.TrackId][]string{}}
}

func (s *State) State(_ context.Context, trackID ids.TrackId) (download.UpgradeStateSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := download.UpgradeStateSnapshot{RejectedSourceKeys: slices.Clone(s.rejected[trackID])}
	if evidence, ok := s.evidence[trackID]; ok {
		snapshot.Evidence = []byte(evidence)
	}
	return snapshot, nil
}

func (s *State) SettleAcquisition(_ context.Context, trackID ids.TrackId, _ download.ClaimNumber, rejectedSourceKeys []string, resetEvidence bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rejected[trackID] = download.AppendRejectedSourceKeys(s.rejected[trackID], rejectedSourceKeys)
	if resetEvidence {
		s.evidence[trackID] = "{}"
	}
	return nil
}
