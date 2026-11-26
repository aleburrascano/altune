package replacement_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	freshURL    = "https://www.youtube.com/watch?v=freshAAAAAA"
	currentURL  = "https://www.youtube.com/watch?v=currentAAAA"
	originalRef = "u/the weeknd/after hours/blinding lights.mp3"
)

type audioStore struct {
	objects map[string]string
}

func (s *audioStore) Exists(_ context.Context, audioRef string) (bool, error) {
	_, ok := s.objects[audioRef]
	return ok, nil
}

func (s *audioStore) Store(_ context.Context, sourcePath string, audioRef string) error {
	data, err := os.ReadFile(sourcePath)
	if err != nil {
		return err
	}
	s.objects[audioRef] = string(data)
	return nil
}

func (s *audioStore) Delete(_ context.Context, audioRef string) error {
	delete(s.objects, audioRef)
	return nil
}

type source struct {
	mu    sync.Mutex
	finds int
}

func (s *source) Name() download.SourceName { return "new-audio" }

func (s *source) Find(context.Context, download.FindRequest) ([]download.AudioCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finds++
	return []download.AudioCandidate{{
		Title: "Blinding Lights", URL: freshURL, Channel: "The Weeknd - Topic", Categories: []string{"Music"},
	}}, nil
}

func (s *source) Fetch(_ context.Context, _ download.AudioCandidate, outDir string) (download.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return download.FetchedAudio{Path: path}, os.WriteFile(path, []byte("new bytes"), 0o600)
}

type upgradeStore struct {
	mu       sync.Mutex
	tries    int
	attempts []download.UpgradeAttempt
	gaveUp   []string
}

func (f *upgradeStore) Candidates(context.Context, download.UpgradeSelection) ([]download.UpgradeCandidate, error) {
	return nil, nil
}

func (f *upgradeStore) NoteAttempt(_ context.Context, _ ids.TrackId, attempt download.UpgradeAttempt) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = append(f.attempts, attempt)
	if attempt.CountsAsTry {
		f.tries++
	}
	return f.tries, nil
}

func (f *upgradeStore) GiveUp(_ context.Context, _ ids.TrackId, _ int, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gaveUp = append(f.gaveUp, reason)
	return nil
}

type publisher struct {
	mu        sync.Mutex
	published int
}

func (p *publisher) Publish(context.Context, ids.UserId, events.Payload) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published++
}

type world struct {
	t        *testing.T
	tracks   *acquisitiontest.Ledger
	audio    *audioStore
	source   *source
	upgrades *upgradeStore
	userId   ids.UserId
	trackId  ids.TrackId
}

func seed(t *testing.T, stored download.Grade) *world {
	t.Helper()
	ref, sourceURL, isrc := originalRef, currentURL, "USUG11904206"
	track := acquisitiontest.LedgerTrack{
		TrackSnapshot: download.TrackSnapshot{
			ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
			Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
			Status: download.TrackReady, AudioRef: &ref, AudioSourceURL: &sourceURL, ISRC: &isrc,
			Evidence: json.RawMessage(fmt.Sprintf(`{"grade":%q,"pipeline_generation":1}`, stored)),
		},
		Confidence: 0.5,
	}
	return &world{
		t:        t,
		tracks:   acquisitiontest.NewLedger(track),
		audio:    &audioStore{objects: map[string]string{originalRef: "original bytes"}},
		source:   &source{},
		upgrades: &upgradeStore{},
		userId:   track.UserId,
		trackId:  track.ID,
	}
}

type unstreamableLedger struct {
	*acquisitiontest.Ledger
}

func (l unstreamableLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*download.TrackSnapshot, error) {
	track, err := l.Ledger.Get(ctx, id, userId)
	if track != nil {
		track.Streamable = false
	}
	return track, err
}

func TestRunner_TrustsTheLedgersStreamableAndSkipsAFileItCallsUnplayable(t *testing.T) {
	w := seed(t, download.GradeB)
	replacer := &gateRecorder{}
	runner := newRunner(t, unstreamableLedger{w.tracks}, replacer, w.upgrades, download.UpgradeConfig{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 0 {
		t.Errorf("gates = %+v, want the file skipped because the ledger reports it unstreamable", replacer.gates)
	}
}

func (w *world) edit(change func(*acquisitiontest.LedgerTrack)) {
	track, _ := w.tracks.Track(w.trackId)
	change(&track)
	w.tracks.Seed(track)
}

func (w *world) asUserUpload() {
	w.edit(func(track *acquisitiontest.LedgerTrack) { track.AudioSourceURL = nil })
}

func (w *world) storedLengths(catalogSeconds, storedSeconds float64) {
	w.edit(func(track *acquisitiontest.LedgerTrack) {
		track.CatalogDurationSeconds, track.StoredDurationSeconds = &catalogSeconds, &storedSeconds
	})
}

func (w *world) runner(cfg download.UpgradeConfig) *download.UpgradeRunner {
	return w.runnerPublishingTo(cfg, events.NoopPublisher())
}

func (w *world) runnerPublishingTo(cfg download.UpgradeConfig, pub events.Publisher) *download.UpgradeRunner {
	return w.runnerChecking(cfg, pub, func(*download.AcquireTrackAudioService) download.UpgradeInPlace { return download.NoInPlaceCheck() })
}

func (w *world) runnerChecking(cfg download.UpgradeConfig, pub events.Publisher, inPlace func(*download.AcquireTrackAudioService) download.UpgradeInPlace) *download.UpgradeRunner {
	stages := noStages(w.audio)
	stages.Events = pub
	svc := download.MustNewAcquireTrackAudioService(w.tracks, download.NewSourceRegistry(w.source), w.audio, download.NewTempWorkspace(), noVerification(), stages)
	runner, err := download.NewUpgradeRunner(w.tracks, svc, w.upgrades, cfg, inPlace(svc))
	if err != nil {
		w.t.Fatalf("NewUpgradeRunner: %v", err)
	}
	return runner
}

func newRunner(t testing.TB, ledger download.TrackLedgerPort, replacer download.UpgradeReplacer, store download.UpgradeStorePort, cfg download.UpgradeConfig) *download.UpgradeRunner {
	t.Helper()
	runner, err := download.NewUpgradeRunner(ledger, replacer, store, cfg, download.NoInPlaceCheck())
	if err != nil {
		t.Fatalf("NewUpgradeRunner: %v", err)
	}
	return runner
}

func TestNewUpgradeRunner_RefusesEachMissingPort(t *testing.T) {
	w := seed(t, download.GradeB)
	cases := map[string]func() (*download.UpgradeRunner, error){
		"ledger": func() (*download.UpgradeRunner, error) {
			return download.NewUpgradeRunner(nil, &gateRecorder{}, w.upgrades, download.UpgradeConfig{}, download.NoInPlaceCheck())
		},
		"replacer": func() (*download.UpgradeRunner, error) {
			return download.NewUpgradeRunner(w.tracks, nil, w.upgrades, download.UpgradeConfig{}, download.NoInPlaceCheck())
		},
		"store": func() (*download.UpgradeRunner, error) {
			return download.NewUpgradeRunner(w.tracks, &gateRecorder{}, nil, download.UpgradeConfig{}, download.NoInPlaceCheck())
		},
	}
	for name, build := range cases {
		runner, err := build()

		if !errors.Is(err, download.ErrMissingRunnerPort) || !strings.Contains(err.Error(), name) || runner != nil {
			t.Errorf("%s: got (%v, %v), want a nil runner and ErrMissingRunnerPort naming it", name, runner, err)
		}
	}
}

type audioDeleter interface {
	Delete(ctx context.Context, audioRef string) error
}

func noStages(store audioDeleter) download.StagePorts {
	return download.StagePorts{
		Prober:        download.NoopAudioProber(),
		Tagger:        download.NoopAudioTagger(),
		Rejections:    download.NoopRejectionStore(),
		StoredAudio:   download.NoopStoredAudioReader(),
		Orphans:       download.MustDirectDeleteOrphans(store),
		Skips:         download.NoopVerifySkipRecorder(),
		Events:        events.NoopPublisher(),
		DownloadLimit: download.Unlimited(),
		Observer:      download.NoObserver(),
		Sleep:         clock.Sleep,
		Now:           time.Now,
		Unit:          random.Real,
	}
}

func noVerification() download.VerificationPorts {
	return download.VerificationPorts{
		Identifier:      download.NoopAudioIdentifier(),
		RecordingLookup: download.NoopRecordingLookup(),
		Rerecordings:    download.NoopRerecordingFinder(),
		Previews:        download.NoopPreviewMatcher(),
		Recordings:      download.NoopRecordingResolver(),
	}
}

func (w *world) committed() acquisitiontest.LedgerTrack {
	track, _ := w.tracks.Track(w.trackId)
	return track
}

func (w *world) committedGrade(t *testing.T) download.Grade {
	t.Helper()
	var stored struct {
		Grade download.Grade `json:"grade"`
	}
	if err := json.Unmarshal(w.committed().Evidence, &stored); err != nil {
		t.Fatalf("committed evidence: %v", err)
	}
	return stored.Grade
}

func TestRunner_ALowerGradeTrackGetsTheBetterFile(t *testing.T) {
	w := seed(t, download.GradeF)

	if err := w.runner(download.UpgradeConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	row := w.committed()
	if row.AudioRef == nil || *row.AudioRef == originalRef || w.audio.objects[*row.AudioRef] != "new bytes" {
		t.Fatalf("audio_ref = %v, want a new ref serving the better file", row.AudioRef)
	}
	if _, stillThere := w.audio.objects[originalRef]; stillThere {
		t.Error("the superseded file must be deleted once the better one is committed")
	}
	if got := w.committedGrade(t); !got.Beats(download.GradeF) {
		t.Errorf("stored grade = %q, want the new file's grade above F", got)
	}
	if len(w.upgrades.attempts) != 0 || len(w.upgrades.gaveUp) != 0 {
		t.Errorf("a successful swap recorded attempts %v / give-ups %v, want none", w.upgrades.attempts, w.upgrades.gaveUp)
	}
}

func TestRunner_AnEqualGradeKeepsTheOldFileAndRejectsTheTriedSource(t *testing.T) {
	w := seed(t, download.GradeD)

	if err := w.runner(download.UpgradeConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); !errors.Is(err, download.ErrUpgradeKept) {
		t.Fatalf("ExecuteUpgrade: %v, want a kept file to be a settled job", err)
	}

	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the old ref %q kept", row.AudioRef, originalRef)
	}
	if len(w.audio.objects) != 1 || w.audio.objects[originalRef] != "original bytes" {
		t.Errorf("objects = %v, want only the untouched original: the new file is discarded", w.audio.objects)
	}
	want := []download.UpgradeAttempt{{RejectedSourceKey: "youtube:freshAAAAAA", CountsAsTry: true}}
	if fmt.Sprint(w.upgrades.attempts) != fmt.Sprint(want) {
		t.Errorf("attempts = %v, want %v: the tried source joins the rejected keys", w.upgrades.attempts, want)
	}
}

func TestRunner_ALowerGradeKeepsTheOldFile(t *testing.T) {
	w := seed(t, download.GradeB)

	if err := w.runner(download.UpgradeConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); !errors.Is(err, download.ErrUpgradeKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want the old file alone", row.AudioRef, w.audio.objects)
	}
	if got := w.committedGrade(t); got != download.GradeB {
		t.Errorf("stored grade = %q, want B untouched", got)
	}
}

func TestRunner_AUserUploadIsNeverReplaced(t *testing.T) {
	w := seed(t, download.GradeF)
	w.asUserUpload()

	if err := w.runner(download.UpgradeConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0 for a track the user uploaded", w.source.finds)
	}
	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want the user's file untouched", row.AudioRef, w.audio.objects)
	}
}

func TestRunner_ATrackThatIsGoneOrNotTheCallersIsSkippedWithoutError(t *testing.T) {
	cases := map[string]func(w *world) (ids.UserId, ids.TrackId){
		"an unknown track":     func(w *world) (ids.UserId, ids.TrackId) { return w.userId, ids.NewTrackId() },
		"another user's track": func(w *world) (ids.UserId, ids.TrackId) { return ids.NewUserId(uuid.New()), w.trackId },
	}
	for name, pick := range cases {
		t.Run(name, func(t *testing.T) {
			w := seed(t, download.GradeF)
			userId, trackId := pick(w)

			if err := w.runner(download.UpgradeConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), userId, trackId); err != nil {
				t.Fatalf("ExecuteUpgrade = %v, want nil: a missing track is skipped, not a failure", err)
			}

			if w.source.finds != 0 || len(w.upgrades.attempts) != 0 || len(w.upgrades.gaveUp) != 0 {
				t.Errorf("finds = %d, attempts = %v, give-ups = %v, want nothing done", w.source.finds, w.upgrades.attempts, w.upgrades.gaveUp)
			}
			if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef {
				t.Errorf("audio_ref = %v, want the seeded track untouched", row.AudioRef)
			}
		})
	}
}

func TestRunner_AGradeATrackIsLeftAlone(t *testing.T) {
	w := seed(t, download.GradeA)

	if err := w.runner(download.UpgradeConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0: nothing grades above A", w.source.finds)
	}
}

func TestRunner_GivesUpAfterTheMaximumTriesWithNothingBetter(t *testing.T) {
	w := seed(t, download.GradeD)
	runner := w.runner(download.UpgradeConfig{MaxTries: 3})

	for try := 1; try <= 3; try++ {
		if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); !errors.Is(err, download.ErrUpgradeKept) {
			t.Fatalf("ExecuteUpgrade try %d: %v", try, err)
		}
		if try < 3 && len(w.upgrades.gaveUp) != 0 {
			t.Fatalf("gave up after %d tries, want to keep trying until 3", try)
		}
	}

	if len(w.upgrades.gaveUp) != 1 || w.upgrades.gaveUp[0] != "no source graded above D after 3 tries" {
		t.Errorf("give-ups = %q, want one with the written reason", w.upgrades.gaveUp)
	}
	if row := w.committed(); *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the old file kept to the end", row.AudioRef)
	}
}

func TestRunner_ASwapThatFailsToCommitKeepsTheOldFileAndCountsNoTry(t *testing.T) {
	w := seed(t, download.GradeF)
	w.tracks.FailWrites(errors.New("transient db error"))

	err := w.runner(download.UpgradeConfig{MaxTries: 1}).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if err == nil {
		t.Fatal("ExecuteUpgrade = nil, want the failed commit reported")
	}
	if w.audio.objects[originalRef] != "original bytes" || len(w.audio.objects) != 1 {
		t.Errorf("objects = %v, want the original untouched and the staged file rolled back", w.audio.objects)
	}
	if row := w.committed(); row.Status != download.TrackReady || *row.AudioRef != originalRef {
		t.Errorf("track = %v %v, want it ready on the original ref", row.Status, row.AudioRef)
	}
	if len(w.upgrades.gaveUp) != 0 || fmt.Sprint(w.upgrades.attempts) != fmt.Sprint([]download.UpgradeAttempt{{}}) {
		t.Errorf("attempts = %v, give-ups = %v, want an infrastructure failure to count for nothing", w.upgrades.attempts, w.upgrades.gaveUp)
	}
}

func TestRunner_PublishesNothingToTheUser(t *testing.T) {
	w := seed(t, download.GradeF)
	pub := &publisher{}

	if err := w.runnerPublishingTo(download.UpgradeConfig{MaxTries: 3}, pub).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if pub.published != 0 {
		t.Errorf("published %d events, want a background upgrade to stay silent", pub.published)
	}
}

func TestReplace_IsNotHeldToTheRatchet(t *testing.T) {
	w := seed(t, download.GradeB)
	svc := download.MustNewAcquireTrackAudioService(w.tracks, download.NewSourceRegistry(w.source), w.audio,
		download.NewTempWorkspace(), noVerification(), noStages(w.audio))

	if err := svc.ExecuteReplace(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if row := w.committed(); *row.AudioRef == originalRef {
		t.Error("a user-asked replace must not be held to the ratchet")
	}
}

type failingGiveUpStore struct {
	upgradeStore
	giveUpCalls int
}

func (f *failingGiveUpStore) GiveUp(context.Context, ids.TrackId, int, string) error {
	f.giveUpCalls++
	return errors.New("db down")
}

func TestRunner_AFailedGiveUpStillSettlesTheKeptFile(t *testing.T) {
	w := seed(t, download.GradeD)
	store := &failingGiveUpStore{}
	svc := download.MustNewAcquireTrackAudioService(w.tracks, download.NewSourceRegistry(w.source), w.audio, download.NewTempWorkspace(), noVerification(), noStages(w.audio))
	runner := newRunner(t, w.tracks, svc, store, download.UpgradeConfig{MaxTries: 1})

	err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if !errors.Is(err, download.ErrUpgradeKept) {
		t.Errorf("ExecuteUpgrade = %v, want the kept outcome even though the give-up could not be written", err)
	}
	if store.giveUpCalls != 1 {
		t.Errorf("GiveUp calls = %d, want 1", store.giveUpCalls)
	}
	if row := w.committed(); *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the old file kept", row.AudioRef)
	}
}

type gateRecorder struct {
	gates []download.ReplacementGate
}

func (g *gateRecorder) ReplaceGated(_ context.Context, _ ids.UserId, _ *download.TrackSnapshot, gate download.ReplacementGate) (download.ReplaceAttempt, error) {
	g.gates = append(g.gates, gate)
	return download.ReplaceAttempt{}, nil
}

func TestRunner_ALowBitrateFileAtAIsNotSettled(t *testing.T) {
	cases := []struct {
		name     string
		evidence string
		reached  bool
	}{
		{"a muxed format 18 file", `{"grade":"A","source_format_id":"18","source_kbps":96,"in_place_generation":%d}`, true},
		{"an audio-only file above the floor", `{"grade":"A","source_format_id":"140","source_kbps":130,"in_place_generation":%d}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := seed(t, download.GradeA)
			w.storedEvidence(fmt.Sprintf(tc.evidence, download.PipelineGeneration))
			replacer := &gateRecorder{}
			runner := newRunner(t, w.tracks, replacer, w.upgrades, download.UpgradeConfig{MaxTries: 3, LowBitrateFloorKbps: 112})

			if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
				t.Fatalf("ExecuteUpgrade: %v", err)
			}

			if !tc.reached {
				if len(replacer.gates) != 0 {
					t.Errorf("gates = %+v, want the file skipped", replacer.gates)
				}
				return
			}
			if len(replacer.gates) != 1 || replacer.gates[0].Stored.ID != "18" {
				t.Errorf("gates = %+v, want one ReplaceGated carrying the stored format 18", replacer.gates)
			}
		})
	}
}

func TestRunner_TheGateCarriesTheCatalogAndStoredLengths(t *testing.T) {
	w := seed(t, download.GradeB)
	w.storedLengths(285, 231)
	replacer := &gateRecorder{}
	runner := newRunner(t, w.tracks, replacer, w.upgrades, download.UpgradeConfig{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 1 || replacer.gates[0].CatalogSeconds != 285 || replacer.gates[0].StoredSeconds != 231 {
		t.Errorf("gates = %+v, want one carrying catalog 285 s and stored 231 s", replacer.gates)
	}
}

func TestRunner_ALowBitrateGateAlsoCarriesTheCatalogAndStoredLengths(t *testing.T) {
	w := seed(t, download.GradeA)
	w.storedEvidence(fmt.Sprintf(`{"grade":"A","source_format_id":"18","source_kbps":96,"in_place_generation":%d}`, download.PipelineGeneration))
	w.storedLengths(195, 292)
	replacer := &gateRecorder{}
	runner := newRunner(t, w.tracks, replacer, w.upgrades, download.UpgradeConfig{MaxTries: 3, LowBitrateFloorKbps: 112})

	if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 1 {
		t.Fatalf("gates = %+v, want one", replacer.gates)
	}
	g := replacer.gates[0]
	if g.Stored.ID != "18" || g.FloorKbps != 112 || g.CatalogSeconds != 195 || g.StoredSeconds != 292 {
		t.Errorf("gate = %+v, want stored format 18, floor 112, catalog 195 s and stored 292 s", g)
	}
}

func TestRunner_ATrackWithNoKnownLengthsGetsAGateWithTheLengthRescueOff(t *testing.T) {
	w := seed(t, download.GradeB)
	replacer := &gateRecorder{}
	runner := newRunner(t, w.tracks, replacer, w.upgrades, download.UpgradeConfig{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 1 || replacer.gates[0].CatalogSeconds != 0 || replacer.gates[0].StoredSeconds != 0 {
		t.Errorf("gates = %+v, want one with catalog and stored lengths 0", replacer.gates)
	}
}
