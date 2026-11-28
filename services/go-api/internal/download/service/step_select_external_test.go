package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
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
	found   []ports.AudioCandidate
	fetched []string
}

func (s *artTracks) Name() domain.SourceName { return "art-tracks" }

func (s *artTracks) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	return s.found, nil
}

func (s *artTracks) Fetch(_ context.Context, c ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	s.mu.Lock()
	s.fetched = append(s.fetched, c.URL)
	s.mu.Unlock()
	path := filepath.Join(outDir, "audio.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("downloaded bytes"), 0o600)
}

func TestSelect_RanksArtTracksByTheResolvedLengthWhenTheTrackHasNone(t *testing.T) {
	artTrack := func(url string, duration float64) ports.AudioCandidate {
		return downloadtest.Candidate(
			url,
			downloadtest.WithTitle("Phone (Interlude)"),
			downloadtest.WithDuration(duration),
			downloadtest.WithArtists([]string{"SLAYR"}),
			downloadtest.WithAlbum("Phone"),
			downloadtest.WithKind(ports.KindArtTrack),
		)
	}
	source := &artTracks{found: []ports.AudioCandidate{
		artTrack("https://music.youtube.com/watch?v=aaaaaaaaaaa", 112),
		artTrack("https://music.youtube.com/watch?v=bbbbbbbbbbb", 128),
	}}
	track := ports.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Phone (Interlude)", Artist: "SLAYR", Album: "Phone", Status: ports.TrackPending,
	}
	svc := service.MustNewAcquireTrackAudioService(&pendingLedger{track: track}, service.NewSourceRegistry(source), storedAudio{},
		scratchfiles.New(),
		resolving(&resolver{identity: ports.RecordingIdentity{Duration: 128}}, noVerification()), noStages())

	_ = svc.Execute(context.Background(), ports.AcquisitionJob{UserID: track.UserId, TrackID: track.ID})

	if len(source.fetched) == 0 || source.fetched[0] != "https://music.youtube.com/watch?v=bbbbbbbbbbb" {
		t.Errorf("fetched %v, want the 128 s art track first: it is the closest to the resolved length", source.fetched)
	}
}
