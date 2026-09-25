package app

import (
	"altune/go-api/internal/acquisition/adapters/fixture"
	"altune/go-api/internal/acquisition/adapters/id3"
	"altune/go-api/internal/acquisition/adapters/ytdlp"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"context"
	"os"
	"path/filepath"
	"testing"

	acqService "altune/go-api/internal/acquisition/service"

	"github.com/google/uuid"
)

type fixtureTrackRepo struct {
	track *domain.Track
}

func (r *fixtureTrackRepo) GetByID(_ context.Context, _ domain.TrackId, _ shared.UserId) (*domain.Track, error) {
	return r.track, nil
}

func (r *fixtureTrackRepo) Update(_ context.Context, track *domain.Track, _ int) error {
	r.track = track
	return nil
}

func (r *fixtureTrackRepo) AudioRefInUse(_ context.Context, _ string, _ domain.TrackId) (bool, error) {
	return false, nil
}

func TestAcquisitionSources_FixtureNeverWiredOutsideOptedInNonProd(t *testing.T) {
	cases := []config.Config{
		{Env: "production", AcquisitionFixtureOptIn: true, YtDLPEnabled: true},
		{Env: "staging", AcquisitionFixtureOptIn: true, YtDLPEnabled: true},
		{Env: "", AcquisitionFixtureOptIn: true, YtDLPEnabled: true},
		{Env: "test", AcquisitionFixtureOptIn: false, YtDLPEnabled: true},
	}
	for _, c := range cases {
		sources, _ := (&App{cfg: &c}).acquisitionSources()
		names := sourceNames(sources)
		if containsSource(names, fixture.SourceName) || !containsSource(names, ytdlp.SourceName) {
			t.Fatalf("cfg env=%q optIn=%v: sources %v, want live sources and no fixture", c.Env, c.AcquisitionFixtureOptIn, names)
		}
	}
}

func TestAcquisitionSources_OptedInTestEnvUsesOnlyFixture(t *testing.T) {
	cfg := &config.Config{Env: "test", AcquisitionFixtureOptIn: true, YtMusicEnabled: true, YtDLPEnabled: true, StreamripServices: []string{"qobuz"}}
	sources, _ := (&App{cfg: cfg}).acquisitionSources()
	if names := sourceNames(sources); len(names) != 1 || names[0] != fixture.SourceName {
		t.Fatalf("sources = %v, want only %q", names, fixture.SourceName)
	}
}

func TestAcquisitionFixture_SavedTrackReachesReadyWithFileInMusicDir(t *testing.T) {
	musicDir := t.TempDir()
	track := acquireWithFixture(t, musicDir)
	assertReadyWithAudio(t, track, musicDir)
}

func TestAcquisitionFixture_FetchedAudioPassesProbeAndDecodeGates(t *testing.T) {
	prober := ytdlp.NewFfprobeProber(os.Getenv("FFMPEG_LOCATION"))
	if ffprobe, ffmpeg := prober.Available(); !ffprobe || !ffmpeg {
		t.Skip("ffprobe/ffmpeg not installed")
	}
	musicDir := t.TempDir()
	track := acquireWithFixture(t, musicDir, acqService.WithAudioProber(prober))
	assertReadyWithAudio(t, track, musicDir)
}

func acquireWithFixture(t *testing.T, musicDir string, opts ...func(*acqService.AcquireTrackAudioService)) *domain.Track {
	t.Helper()
	sources, _ := (&App{cfg: &config.Config{Env: "test", AcquisitionFixtureOptIn: true}}).acquisitionSources()
	userID := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userID, "Fixture Song", "Fixture Artist", "")
	if err != nil || track.SetDuration(212) != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := &fixtureTrackRepo{track: track}
	opts = append(opts, acqService.WithAudioTagger(id3.NewTagger()))
	svc := acqService.NewAcquireTrackAudioService(repo, acqService.NewSourceRegistry(sources...), storage.NewFilesystemAudioStore(musicDir), opts...)
	if err := svc.Execute(context.Background(), userID, track.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return repo.track
}

func assertReadyWithAudio(t *testing.T, track *domain.Track, musicDir string) {
	t.Helper()
	if track.AcquisitionStatus != domain.AcquisitionReady || track.AudioRef == nil {
		t.Fatalf("status = %v audioRef = %v, want ready with a ref", track.AcquisitionStatus, track.AudioRef)
	}
	stored, err := os.Stat(filepath.Join(musicDir, *track.AudioRef))
	if err != nil || stored.Size() == 0 {
		t.Fatalf("stored audio %q: size %v err %v", *track.AudioRef, stored, err)
	}
}
