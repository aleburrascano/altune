package service_test

import (
	"altune/go-api/internal/acquisition"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type artTracks struct {
	mu      sync.Mutex
	found   []acquisition.AudioCandidate
	fetched []string
}

func (s *artTracks) Name() acquisition.SourceName { return "art-tracks" }

func (s *artTracks) Find(context.Context, acquisition.FindRequest) ([]acquisition.AudioCandidate, error) {
	return s.found, nil
}

func (s *artTracks) Fetch(_ context.Context, c acquisition.AudioCandidate, outDir string) (acquisition.FetchedAudio, error) {
	s.mu.Lock()
	s.fetched = append(s.fetched, c.URL)
	s.mu.Unlock()
	path := filepath.Join(outDir, "audio.mp3")
	return acquisition.FetchedAudio{Path: path}, os.WriteFile(path, []byte("downloaded bytes"), 0o600)
}

func TestSelect_RanksArtTracksByTheResolvedLengthWhenTheTrackHasNone(t *testing.T) {
	artTrack := func(url string, duration float64) acquisition.AudioCandidate {
		return acquisition.AudioCandidate{
			Title: "Phone (Interlude)", URL: url, Duration: duration,
			Artists: []string{"SLAYR"}, Album: "Phone", Kind: acquisition.KindArtTrack,
		}
	}
	source := &artTracks{found: []acquisition.AudioCandidate{
		artTrack("https://music.youtube.com/watch?v=aaaaaaaaaaa", 112),
		artTrack("https://music.youtube.com/watch?v=bbbbbbbbbbb", 128),
	}}
	track := acquisition.TrackSnapshot{
		ID: shared.NewTrackId(), UserId: shared.NewUserId(uuid.New()),
		Title: "Phone (Interlude)", Artist: "SLAYR", Album: "Phone", Status: acquisition.TrackPending,
	}
	svc := acquisition.NewAcquireTrackAudioService(&pendingLedger{track: track}, acquisition.NewSourceRegistry(source), storedAudio{},
		acquisition.WithAcquireTempWorkspace(acquisition.NewTempWorkspace()),
		acquisition.WithRecordingResolver(&resolver{identity: acquisition.RecordingIdentity{Duration: 128}}))

	_ = svc.Execute(context.Background(), track.UserId, track.ID)

	if len(source.fetched) == 0 || source.fetched[0] != "https://music.youtube.com/watch?v=bbbbbbbbbbb" {
		t.Errorf("fetched %v, want the 128 s art track first: it is the closest to the resolved length", source.fetched)
	}
}
