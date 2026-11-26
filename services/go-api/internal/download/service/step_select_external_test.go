package service_test

import (
	"altune/go-api/internal/download"
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
	found   []download.AudioCandidate
	fetched []string
}

func (s *artTracks) Name() download.SourceName { return "art-tracks" }

func (s *artTracks) Find(context.Context, download.FindRequest) ([]download.AudioCandidate, error) {
	return s.found, nil
}

func (s *artTracks) Fetch(_ context.Context, c download.AudioCandidate, outDir string) (download.FetchedAudio, error) {
	s.mu.Lock()
	s.fetched = append(s.fetched, c.URL)
	s.mu.Unlock()
	path := filepath.Join(outDir, "audio.mp3")
	return download.FetchedAudio{Path: path}, os.WriteFile(path, []byte("downloaded bytes"), 0o600)
}

func TestSelect_RanksArtTracksByTheResolvedLengthWhenTheTrackHasNone(t *testing.T) {
	artTrack := func(url string, duration float64) download.AudioCandidate {
		return download.AudioCandidate{
			Title: "Phone (Interlude)", URL: url, Duration: duration,
			Artists: []string{"SLAYR"}, Album: "Phone", Kind: download.KindArtTrack,
		}
	}
	source := &artTracks{found: []download.AudioCandidate{
		artTrack("https://music.youtube.com/watch?v=aaaaaaaaaaa", 112),
		artTrack("https://music.youtube.com/watch?v=bbbbbbbbbbb", 128),
	}}
	track := download.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Phone (Interlude)", Artist: "SLAYR", Album: "Phone", Status: download.TrackPending,
	}
	svc := download.MustNewAcquireTrackAudioService(&pendingLedger{track: track}, download.NewSourceRegistry(source), storedAudio{},
		download.NewTempWorkspace(),
		resolving(&resolver{identity: download.RecordingIdentity{Duration: 128}}, noVerification()), noStages())

	_ = svc.Execute(context.Background(), track.UserId, track.ID)

	if len(source.fetched) == 0 || source.fetched[0] != "https://music.youtube.com/watch?v=bbbbbbbbbbb" {
		t.Errorf("fetched %v, want the 128 s art track first: it is the closest to the resolved length", source.fetched)
	}
}
