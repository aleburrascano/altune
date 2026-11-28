package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/ids"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type artTracks struct {
	mu      sync.Mutex
	found   []domain.AudioCandidate
	fetched []string
}

func (s *artTracks) Name() domain.SourceName { return "art-tracks" }

func (s *artTracks) Find(context.Context, domain.FindRequest) ([]domain.AudioCandidate, error) {
	return s.found, nil
}

func (s *artTracks) Fetch(_ context.Context, c domain.AudioCandidate, outDir string) (domain.FetchedAudio, error) {
	s.mu.Lock()
	s.fetched = append(s.fetched, c.URL)
	s.mu.Unlock()
	path := filepath.Join(outDir, "audio.mp3")
	return domain.FetchedAudio{Path: path}, os.WriteFile(path, []byte("downloaded bytes"), 0o600)
}

func TestSelect_RanksArtTracksByTheResolvedLengthWhenTheTrackHasNone(t *testing.T) {
	artTrack := func(url string, duration float64) domain.AudioCandidate {
		return downloadtest.Candidate(
			url,
			downloadtest.WithTitle("Phone (Interlude)"),
			downloadtest.WithDuration(duration),
			downloadtest.WithArtists([]string{"SLAYR"}),
			downloadtest.WithAlbum("Phone"),
			downloadtest.WithKind(domain.KindArtTrack),
		)
	}
	source := &artTracks{found: []domain.AudioCandidate{
		artTrack("https://music.youtube.com/watch?v=aaaaaaaaaaa", 112),
		artTrack("https://music.youtube.com/watch?v=bbbbbbbbbbb", 128),
	}}
	track := domain.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Phone (Interlude)", Artist: "SLAYR", Album: "Phone", Status: domain.TrackPending,
	}
	svc := service.MustNewAcquireTrackAudioService(&pendingLedger{track: track}, service.NewSourceRegistry(source), storedAudio{},
		scratchfiles.New(),
		resolving(&resolver{identity: domain.RecordingIdentity{Duration: 128}}, noVerification()), noStages())

	_ = svc.Execute(context.Background(), domain.AcquisitionJob{UserID: track.UserId, TrackID: track.ID})

	if len(source.fetched) == 0 || source.fetched[0] != "https://music.youtube.com/watch?v=bbbbbbbbbbb" {
		t.Errorf("fetched %v, want the 128 s art track first: it is the closest to the resolved length", source.fetched)
	}
}
