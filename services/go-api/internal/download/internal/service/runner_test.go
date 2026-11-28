package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/evidence"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
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

func (s *source) Name() domain.SourceName { return "new-audio" }

func (s *source) Find(context.Context, domain.FindRequest) ([]domain.AudioCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finds++
	return []domain.AudioCandidate{downloadtest.Candidate(
		freshURL,
		downloadtest.WithTitle("Blinding Lights"),
		downloadtest.WithChannel("The Weeknd - Topic"),
		downloadtest.WithCategories([]string{"Music"}),
	)}, nil
}

func (s *source) Fetch(_ context.Context, _ domain.AudioCandidate, outDir string) (domain.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return domain.FetchedAudio{Path: path}, os.WriteFile(path, []byte("new bytes"), 0o600)
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
	tracks   *memory.Ledger
	audio    *audioStore
	source   *source
	upgrades *memory.UpgradeState
	userId   ids.UserId
	trackId  ids.TrackId
}

func seed(t *testing.T, stored domain.Grade) *world {
	t.Helper()
	ref, sourceURL, isrc := originalRef, currentURL, "USUG11904206"
	track := memory.LedgerTrack{
		TrackSnapshot: domain.TrackSnapshot{
			ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New()),
			Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
			Status: domain.TrackReady, Streamable: true, AudioRef: &ref, AudioSourceURL: &sourceURL, ISRC: &isrc,
			Evidence: json.RawMessage(fmt.Sprintf(`{"grade":%q,"pipeline_generation":1}`, stored)),
		},
		Confidence: 0.5,
	}
	return &world{
		t:        t,
		tracks:   memory.NewLedger(track),
		audio:    &audioStore{objects: map[string]string{originalRef: "original bytes"}},
		source:   &source{},
		upgrades: memory.NewUpgradeState(time.Now),
		userId:   track.UserId,
		trackId:  track.ID,
	}
}

type unstreamableLedger struct {
	*memory.Ledger
}

func (l unstreamableLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*domain.TrackSnapshot, error) {
	track, err := l.Ledger.Get(ctx, id, userId)
	if track != nil {
		track.Streamable = false
	}
	return track, err
}

func TestRunner_TrustsTheLedgersStreamableAndSkipsAFileItCallsUnplayable(t *testing.T) {
	w := seed(t, domain.GradeB)
	replacer := &replaceGateRecorder{}
	runner := newRunner(t, unstreamableLedger{w.tracks}, replacer, w.upgrades, service.RunnerConfig{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 0 {
		t.Errorf("gates = %+v, want the file skipped because the ledger reports it unstreamable", replacer.gates)
	}
}

func (w *world) edit(change func(*memory.LedgerTrack)) {
	track, _ := w.tracks.Track(w.trackId)
	change(&track)
	w.tracks.Seed(track)
}

func (w *world) asUserUpload() {
	w.edit(func(track *memory.LedgerTrack) { track.AudioSourceURL = nil })
}

func (w *world) storedLengths(catalogSeconds, storedSeconds float64) {
	w.edit(func(track *memory.LedgerTrack) {
		track.CatalogDurationSeconds, track.StoredDurationSeconds = &catalogSeconds, &storedSeconds
	})
}

func (w *world) runner(cfg service.RunnerConfig) *service.Runner {
	return w.runnerPublishingTo(cfg, events.NoopPublisher())
}

func (w *world) runnerPublishingTo(cfg service.RunnerConfig, pub events.Publisher) *service.Runner {
	return w.runnerChecking(cfg, pub, func(*service.AcquireTrackAudioService) service.InPlace { return service.NoInPlaceCheck() })
}

func (w *world) runnerChecking(cfg service.RunnerConfig, pub events.Publisher, inPlace func(*service.AcquireTrackAudioService) service.InPlace) *service.Runner {
	stages := replaceNoStages(w.audio)
	stages.Events = pub
	svc := service.MustNewAcquireTrackAudioService(w.tracks, service.NewSourceRegistry(w.source), w.audio, scratchfiles.New(), replaceNoVerification(), stages)
	runner, err := service.NewRunner(w.tracks, service.NewReplaceService(svc), w.upgrades, evidence.NewCodec(), cfg, inPlace(svc))
	if err != nil {
		w.t.Fatalf("NewUpgradeRunner: %v", err)
	}
	return runner
}

func newRunner(t testing.TB, ledger ports.LibraryTracks, replacer service.Replacer, store ports.UpgradeStore, cfg service.RunnerConfig) *service.Runner {
	t.Helper()
	runner, err := service.NewRunner(ledger, replacer, store, evidence.NewCodec(), cfg, service.NoInPlaceCheck())
	if err != nil {
		t.Fatalf("NewUpgradeRunner: %v", err)
	}
	return runner
}

func TestNewUpgradeRunner_RefusesEachMissingPort(t *testing.T) {
	w := seed(t, domain.GradeB)
	cases := map[string]func() (*service.Runner, error){
		"ledger": func() (*service.Runner, error) {
			return service.NewRunner(nil, &replaceGateRecorder{}, w.upgrades, evidence.NewCodec(), service.RunnerConfig{}, service.NoInPlaceCheck())
		},
		"replacer": func() (*service.Runner, error) {
			return service.NewRunner(w.tracks, nil, w.upgrades, evidence.NewCodec(), service.RunnerConfig{}, service.NoInPlaceCheck())
		},
		"store": func() (*service.Runner, error) {
			return service.NewRunner(w.tracks, &replaceGateRecorder{}, nil, evidence.NewCodec(), service.RunnerConfig{}, service.NoInPlaceCheck())
		},
	}
	for name, build := range cases {
		runner, err := build()

		if !errors.Is(err, service.ErrMissingRunnerPort) || !strings.Contains(err.Error(), name) || runner != nil {
			t.Errorf("%s: got (%v, %v), want a nil runner and ErrMissingRunnerPort naming it", name, runner, err)
		}
	}
}

type replaceAudioDeleter interface {
	Delete(ctx context.Context, audioRef string) error
}

func replaceNoStages(store replaceAudioDeleter) service.StagePorts {
	return service.StagePorts{
		Prober:        downloadtest.SilentAudioProber(),
		Tagger:        memory.NoopAudioTagger(),
		Rejections:    memory.NoopRejectionStore(),
		StoredAudio:   memory.NoopStoredAudioReader(),
		Orphans:       service.MustDirectDeleteOrphans(store),
		Skips:         memory.NoopVerifySkipRecorder(),
		Evidence:      evidence.NewCodec(),
		Events:        events.NoopPublisher(),
		DownloadLimit: service.Unlimited(),
		Observer:      service.NoObserver(),
		Sleep:         clock.Sleep,
		Now:           time.Now,
		Unit:          random.Real,
	}
}

func replaceNoVerification() service.VerificationPorts {
	return service.VerificationPorts{
		Identifier:      memory.NoopAudioIdentifier(),
		RecordingLookup: memory.NoopRecordingLookup(),
		Rerecordings:    memory.NoopRerecordingFinder(),
		Previews:        memory.NoopPreviewMatcher(),
		Recordings:      memory.NoopRecordingResolver(),
	}
}

func (w *world) committed() memory.LedgerTrack {
	track, _ := w.tracks.Track(w.trackId)
	return track
}

func (w *world) committedGrade(t *testing.T) domain.Grade {
	t.Helper()
	var stored struct {
		Grade domain.Grade `json:"grade"`
	}
	if err := json.Unmarshal(w.committed().Evidence, &stored); err != nil {
		t.Fatalf("committed evidence: %v", err)
	}
	return stored.Grade
}

func TestRunner_ALowerGradeTrackGetsTheBetterFile(t *testing.T) {
	w := seed(t, domain.GradeF)

	if err := w.runner(service.RunnerConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	row := w.committed()
	if row.AudioRef == nil || *row.AudioRef == originalRef || w.audio.objects[*row.AudioRef] != "new bytes" {
		t.Fatalf("audio_ref = %v, want a new ref serving the better file", row.AudioRef)
	}
	if _, stillThere := w.audio.objects[originalRef]; stillThere {
		t.Error("the superseded file must be deleted once the better one is committed")
	}
	if got := w.committedGrade(t); !got.Beats(domain.GradeF) {
		t.Errorf("stored grade = %q, want the new file's grade above F", got)
	}
	if len(w.upgrades.Attempts()) != 0 || len(w.upgrades.GaveUp()) != 0 {
		t.Errorf("a successful swap recorded attempts %v / give-ups %v, want none", w.upgrades.Attempts(), w.upgrades.GaveUp())
	}
}

func TestRunner_AnEqualGradeKeepsTheOldFileAndRejectsTheTriedSource(t *testing.T) {
	w := seed(t, domain.GradeD)

	if err := w.runner(service.RunnerConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); !errors.Is(err, service.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v, want a kept file to be a settled job", err)
	}

	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the old ref %q kept", row.AudioRef, originalRef)
	}
	if len(w.audio.objects) != 1 || w.audio.objects[originalRef] != "original bytes" {
		t.Errorf("objects = %v, want only the untouched original: the new file is discarded", w.audio.objects)
	}
	want := []domain.UpgradeAttempt{{RejectedSourceKey: "youtube:freshAAAAAA", CountsAsTry: true}}
	if fmt.Sprint(w.upgrades.Attempts()) != fmt.Sprint(want) {
		t.Errorf("attempts = %v, want %v: the tried source joins the rejected keys", w.upgrades.Attempts(), want)
	}
}

func TestRunner_ALowerGradeKeepsTheOldFile(t *testing.T) {
	w := seed(t, domain.GradeB)

	if err := w.runner(service.RunnerConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); !errors.Is(err, service.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want the old file alone", row.AudioRef, w.audio.objects)
	}
	if got := w.committedGrade(t); got != domain.GradeB {
		t.Errorf("stored grade = %q, want B untouched", got)
	}
}

func TestRunner_AUserUploadIsNeverReplaced(t *testing.T) {
	w := seed(t, domain.GradeF)
	w.asUserUpload()

	if err := w.runner(service.RunnerConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
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
			w := seed(t, domain.GradeF)
			userId, trackId := pick(w)

			if err := w.runner(service.RunnerConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: userId, TrackID: trackId}); err != nil {
				t.Fatalf("ExecuteUpgrade = %v, want nil: a missing track is skipped, not a failure", err)
			}

			if w.source.finds != 0 || len(w.upgrades.Attempts()) != 0 || len(w.upgrades.GaveUp()) != 0 {
				t.Errorf("finds = %d, attempts = %v, give-ups = %v, want nothing done", w.source.finds, w.upgrades.Attempts(), w.upgrades.GaveUp())
			}
			if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef {
				t.Errorf("audio_ref = %v, want the seeded track untouched", row.AudioRef)
			}
		})
	}
}

func TestRunner_AGradeATrackIsLeftAlone(t *testing.T) {
	w := seed(t, domain.GradeA)

	if err := w.runner(service.RunnerConfig{MaxTries: 3}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0: nothing grades above A", w.source.finds)
	}
}

func TestRunner_GivesUpAfterTheMaximumTriesWithNothingBetter(t *testing.T) {
	w := seed(t, domain.GradeD)
	runner := w.runner(service.RunnerConfig{MaxTries: 3})

	for try := 1; try <= 3; try++ {
		if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); !errors.Is(err, service.ErrKept) {
			t.Fatalf("ExecuteUpgrade try %d: %v", try, err)
		}
		if try < 3 && len(w.upgrades.GaveUp()) != 0 {
			t.Fatalf("gave up after %d tries, want to keep trying until 3", try)
		}
	}

	if len(w.upgrades.GaveUp()) != 1 || w.upgrades.GaveUp()[0] != "no source graded above D after 3 tries" {
		t.Errorf("give-ups = %q, want one with the written reason", w.upgrades.GaveUp())
	}
	if row := w.committed(); *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the old file kept to the end", row.AudioRef)
	}
}

func TestRunner_ASwapThatFailsToCommitKeepsTheOldFileAndCountsNoTry(t *testing.T) {
	w := seed(t, domain.GradeF)
	w.tracks.FailWrites(errors.New("transient db error"))

	err := w.runner(service.RunnerConfig{MaxTries: 1}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err == nil {
		t.Fatal("ExecuteUpgrade = nil, want the failed commit reported")
	}
	if w.audio.objects[originalRef] != "original bytes" || len(w.audio.objects) != 1 {
		t.Errorf("objects = %v, want the original untouched and the staged file rolled back", w.audio.objects)
	}
	if row := w.committed(); row.Status != domain.TrackReady || *row.AudioRef != originalRef {
		t.Errorf("track = %v %v, want it ready on the original ref", row.Status, row.AudioRef)
	}
	if len(w.upgrades.GaveUp()) != 0 || fmt.Sprint(w.upgrades.Attempts()) != fmt.Sprint([]domain.UpgradeAttempt{{}}) {
		t.Errorf("attempts = %v, give-ups = %v, want an infrastructure failure to count for nothing", w.upgrades.Attempts(), w.upgrades.GaveUp())
	}
}

func TestRunner_PublishesNothingToTheUser(t *testing.T) {
	w := seed(t, domain.GradeF)
	pub := &publisher{}

	if err := w.runnerPublishingTo(service.RunnerConfig{MaxTries: 3}, pub).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if pub.published != 0 {
		t.Errorf("published %d events, want a background upgrade to stay silent", pub.published)
	}
}

func TestReplace_IsNotHeldToTheRatchet(t *testing.T) {
	w := seed(t, domain.GradeB)
	svc := service.MustNewAcquireTrackAudioService(w.tracks, service.NewSourceRegistry(w.source), w.audio,
		scratchfiles.New(), replaceNoVerification(), replaceNoStages(w.audio))

	if err := service.NewReplaceService(svc).ExecuteReplace(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if row := w.committed(); *row.AudioRef == originalRef {
		t.Error("a user-asked replace must not be held to the ratchet")
	}
}

type failingGiveUpStore struct {
	*memory.UpgradeState
	giveUpCalls int
}

func (f *failingGiveUpStore) GiveUp(context.Context, ids.TrackId, int, string) error {
	f.giveUpCalls++
	return errors.New("db down")
}

func TestRunner_AFailedGiveUpStillSettlesTheKeptFile(t *testing.T) {
	w := seed(t, domain.GradeD)
	store := &failingGiveUpStore{UpgradeState: memory.NewUpgradeState(time.Now)}
	svc := service.MustNewAcquireTrackAudioService(w.tracks, service.NewSourceRegistry(w.source), w.audio, scratchfiles.New(), replaceNoVerification(), replaceNoStages(w.audio))
	runner := newRunner(t, w.tracks, service.NewReplaceService(svc), store, service.RunnerConfig{MaxTries: 1})

	err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if !errors.Is(err, service.ErrKept) {
		t.Errorf("ExecuteUpgrade = %v, want the kept outcome even though the give-up could not be written", err)
	}
	if store.giveUpCalls != 1 {
		t.Errorf("GiveUp calls = %d, want 1", store.giveUpCalls)
	}
	if row := w.committed(); *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the old file kept", row.AudioRef)
	}
}

type replaceGateRecorder struct {
	gates []service.ReplacementGate
}

func (g *replaceGateRecorder) ReplaceGated(_ context.Context, job domain.AcquisitionJob, _ *domain.TrackSnapshot, gate service.ReplacementGate) (service.ReplaceAttempt, error) {
	g.gates = append(g.gates, gate)
	return service.ReplaceAttempt{}, nil
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
			w := seed(t, domain.GradeA)
			w.storedEvidence(fmt.Sprintf(tc.evidence, domain.PipelineGeneration))
			replacer := &replaceGateRecorder{}
			runner := newRunner(t, w.tracks, replacer, w.upgrades, service.RunnerConfig{MaxTries: 3, LowBitrateFloorKbps: 112})

			if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
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
	w := seed(t, domain.GradeB)
	w.storedLengths(285, 231)
	replacer := &replaceGateRecorder{}
	runner := newRunner(t, w.tracks, replacer, w.upgrades, service.RunnerConfig{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 1 || replacer.gates[0].CatalogSeconds != 285 || replacer.gates[0].StoredSeconds != 231 {
		t.Errorf("gates = %+v, want one carrying catalog 285 s and stored 231 s", replacer.gates)
	}
}

func TestRunner_ALowBitrateGateAlsoCarriesTheCatalogAndStoredLengths(t *testing.T) {
	w := seed(t, domain.GradeA)
	w.storedEvidence(fmt.Sprintf(`{"grade":"A","source_format_id":"18","source_kbps":96,"in_place_generation":%d}`, domain.PipelineGeneration))
	w.storedLengths(195, 292)
	replacer := &replaceGateRecorder{}
	runner := newRunner(t, w.tracks, replacer, w.upgrades, service.RunnerConfig{MaxTries: 3, LowBitrateFloorKbps: 112})

	if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
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
	w := seed(t, domain.GradeB)
	replacer := &replaceGateRecorder{}
	runner := newRunner(t, w.tracks, replacer, w.upgrades, service.RunnerConfig{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 1 || replacer.gates[0].CatalogSeconds != 0 || replacer.gates[0].StoredSeconds != 0 {
		t.Errorf("gates = %+v, want one with catalog and stored lengths 0", replacer.gates)
	}
}
