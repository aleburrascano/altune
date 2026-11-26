package service_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type recordingTagger struct {
	mu    sync.Mutex
	paths []string
	tags  []ports.TrackTags
	err   error
}

func (r *recordingTagger) Tag(_ context.Context, filePath string, tags ports.TrackTags) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, filePath)
	r.tags = append(r.tags, tags)
	return r.err
}

func (r *recordingTagger) calls() ([]string, []ports.TrackTags) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.paths...), append([]ports.TrackTags(nil), r.tags...)
}

func pendingBlindingLights() ports.TrackSnapshot {
	return ports.TrackSnapshot{
		ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
		Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours", Status: ports.TrackPending,
	}
}

func acquireWith(t *testing.T, track ports.TrackSnapshot, source ports.AudioSource, opts ...func(*service.AcquireTrackAudioService)) (*acquisitiontest.Ledger, error) {
	t.Helper()
	return acquireVerified(t, track, source, noVerification(), noStages(), opts...)
}

func acquireStaged(t *testing.T, track ports.TrackSnapshot, source ports.AudioSource, stages service.StagePorts, opts ...func(*service.AcquireTrackAudioService)) (*acquisitiontest.Ledger, error) {
	t.Helper()
	return acquireVerified(t, track, source, noVerification(), stages, opts...)
}

func acquireVerified(t *testing.T, track ports.TrackSnapshot, source ports.AudioSource, wiring service.VerificationPorts, stages service.StagePorts, opts ...func(*service.AcquireTrackAudioService)) (*acquisitiontest.Ledger, error) {
	t.Helper()
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})
	svc := service.MustNewAcquireTrackAudioService(ledger, service.NewSourceRegistry(source), &objectStore{objects: map[string]string{}}, scratchfiles.New(), wiring, stages, opts...)
	return ledger, svc.Execute(context.Background(), track.UserId, track.ID)
}

func requireReady(t *testing.T, ledger *acquisitiontest.Ledger, id ids.TrackId, err error) {
	t.Helper()
	got, _ := ledger.Track(id)
	if err != nil || got.Status != ports.TrackReady {
		t.Fatalf("Execute = %v, status = %v, want the track acquired", err, got.Status)
	}
}

type pathlessSource struct{ freshAudioSource }

func (pathlessSource) Name() acqdomain.SourceName { return "pathless" }

func (pathlessSource) Fetch(context.Context, ports.AudioCandidate, string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, nil
}

func TestExecute_ADownloadWithoutAFileFailsTheAcquisitionAndIsNeverTagged(t *testing.T) {
	tagger := &recordingTagger{}
	track := pendingBlindingLights()

	ledger, err := acquireStaged(t, track, pathlessSource{}, noStages(taggedBy(tagger)))

	if err == nil || !strings.Contains(err.Error(), "fetcher returned no file") {
		t.Fatalf("Execute = %v, want the download to fail because the fetcher returned no file", err)
	}
	if got, _ := ledger.Track(track.ID); got.Status == ports.TrackReady {
		t.Errorf("status = %v, want the track not acquired", got.Status)
	}
	if paths, _ := tagger.calls(); len(paths) != 0 {
		t.Fatalf("tagger called for a download without a file: %v", paths)
	}
}

func TestExecute_WithoutATaggerTheTrackIsStillAcquired(t *testing.T) {
	track := pendingBlindingLights()

	ledger, err := acquireWith(t, track, freshAudioSource{})

	requireReady(t, ledger, track.ID, err)
}

func TestExecute_ATaggingFailureIsSwallowed(t *testing.T) {
	track := pendingBlindingLights()

	ledger, err := acquireStaged(t, track, freshAudioSource{}, noStages(taggedBy(&recordingTagger{err: errors.New("boom")})))

	requireReady(t, ledger, track.ID, err)
}

func TestExecute_TheTaggingFailureLogRedactsHostPaths(t *testing.T) {
	logs := captureLog(t)
	tagger := &recordingTagger{err: errors.New("open /run/secrets/altune/yt_cookies.txt: permission denied")}
	track := pendingBlindingLights()

	ledger, err := acquireStaged(t, track, freshAudioSource{}, noStages(taggedBy(tagger)))

	requireReady(t, ledger, track.ID, err)
	logged := logs.String()
	if !strings.Contains(logged, "tagging_failed") {
		t.Fatalf("expected the tagging failure log, got:\n%s", logged)
	}
	if strings.Contains(logged, "/run/secrets") {
		t.Fatalf("a host path leaked into the log:\n%s", logged)
	}
	if !strings.Contains(logged, "permission denied") {
		t.Fatalf("redaction dropped the diagnostic text:\n%s", logged)
	}
}

func TestExecute_TagsTheDownloadedFileWithTheTracksMetadata(t *testing.T) {
	track := pendingBlindingLights()
	albumArtist, genre, year, number := "The Weeknd", "Pop", 2020, 4
	track.AlbumArtist, track.Genre, track.Year, track.TrackNumber = &albumArtist, &genre, &year, &number
	tagger := &recordingTagger{}

	ledger, err := acquireStaged(t, track, freshAudioSource{}, noStages(taggedBy(tagger)))

	requireReady(t, ledger, track.ID, err)
	paths, tags := tagger.calls()
	if len(paths) != 1 || !strings.HasSuffix(paths[0], "audio.mp3") {
		t.Fatalf("tagged %v, want the downloaded audio.mp3 once", paths)
	}
	want := ports.TrackTags{
		Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
		AlbumArtist: "The Weeknd", Genre: "Pop", Year: 2020, TrackNumber: 4,
	}
	if tags[0] != want {
		t.Errorf("tags = %+v, want %+v", tags[0], want)
	}
}

func TestExecute_EmbedsOnlyTheProvenISRC(t *testing.T) {
	const isrc = "USRC17607839"
	resolved := &resolver{identity: ports.RecordingIdentity{ISRC: isrc, MBID: "rec-1", MBIDs: []string{"rec-1"}}}
	pin := "rec-1"
	for name, tc := range map[string]struct {
		catalogMBID *string
		want        string
	}{
		"the catalog pins the resolved recording": {&pin, isrc},
		"an unproven resolver ISRC":               {nil, ""},
	} {
		track := pendingBlindingLights()
		track.CatalogMBID = tc.catalogMBID
		tagger := &recordingTagger{}

		ledger, err := acquireVerified(t, track, freshAudioSource{}, resolving(resolved, noVerification()), noStages(taggedBy(tagger)))

		requireReady(t, ledger, track.ID, err)
		if _, tags := tagger.calls(); len(tags) != 1 || tags[0].ISRC != tc.want {
			t.Errorf("%s: tags = %+v, want one call with ISRC %q", name, tags, tc.want)
		}
	}
}
