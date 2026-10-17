package upgrade_test

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/adapters/tempfs"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/acquisition/service/upgrade"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

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

type trackStore struct {
	rows      map[string]domain.Track
	updateErr error
}

func rowKey(id domain.TrackId, userId shared.UserId) string {
	return id.String() + ":" + userId.String()
}

func (r *trackStore) GetByID(_ context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error) {
	row, ok := r.rows[rowKey(id, userId)]
	if !ok {
		return nil, nil
	}
	return &row, nil
}

func (r *trackStore) Update(_ context.Context, track *domain.Track, _ int) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	r.rows[rowKey(track.ID, track.UserId)] = *track
	return nil
}

func (r *trackStore) AudioRefInUse(_ context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error) {
	for _, row := range r.rows {
		if row.ID != excludeTrackID && row.AudioRef != nil && *row.AudioRef == audioRef {
			return true, nil
		}
	}
	return false, nil
}

type source struct {
	mu    sync.Mutex
	finds int
}

func (s *source) Name() acqdomain.SourceName { return "new-audio" }

func (s *source) Find(context.Context, ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finds++
	return []ports.AudioCandidate{{
		Title: "Blinding Lights", URL: freshURL, Channel: "The Weeknd - Topic", Categories: []string{"Music"},
	}}, nil
}

func (s *source) Fetch(_ context.Context, _ ports.AudioCandidate, outDir string) (ports.FetchedAudio, error) {
	path := filepath.Join(outDir, "audio.mp3")
	return ports.FetchedAudio{Path: path}, os.WriteFile(path, []byte("new bytes"), 0o600)
}

type upgradeStore struct {
	mu       sync.Mutex
	tries    int
	attempts []ports.UpgradeAttempt
	gaveUp   []string
}

func (f *upgradeStore) Candidates(context.Context, ports.UpgradeSelection) ([]ports.UpgradeCandidate, error) {
	return nil, nil
}

func (f *upgradeStore) NoteAttempt(_ context.Context, _ shared.TrackId, attempt ports.UpgradeAttempt) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = append(f.attempts, attempt)
	if attempt.CountsAsTry {
		f.tries++
	}
	return f.tries, nil
}

func (f *upgradeStore) GiveUp(_ context.Context, _ shared.TrackId, _ int, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gaveUp = append(f.gaveUp, reason)
	return nil
}

type publisher struct {
	mu        sync.Mutex
	published int
}

func (p *publisher) Publish(context.Context, shared.UserId, events.EventType, map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.published++
}

type world struct {
	tracks   *trackStore
	audio    *audioStore
	source   *source
	upgrades *upgradeStore
	userId   shared.UserId
	trackId  domain.TrackId
}

func seed(t *testing.T, stored grade.Grade) *world {
	t.Helper()
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkReady(originalRef); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	track.SetAudioSource(currentURL)
	track.AdoptISRC("USUG11904206")
	evidence := json.RawMessage(fmt.Sprintf(`{"grade":%q,"pipeline_generation":1}`, stored))
	if err := track.SetAcquisitionConfidence(0.5, evidence); err != nil {
		t.Fatalf("set evidence: %v", err)
	}
	return &world{
		tracks:   &trackStore{rows: map[string]domain.Track{rowKey(track.ID, userId): *track}},
		audio:    &audioStore{objects: map[string]string{originalRef: "original bytes"}},
		source:   &source{},
		upgrades: &upgradeStore{},
		userId:   userId,
		trackId:  track.ID,
	}
}

func (w *world) asUserUpload() {
	key := rowKey(w.trackId, w.userId)
	row := w.tracks.rows[key]
	row.AudioSourceURL = nil
	w.tracks.rows[key] = row
}

func (w *world) runner(cfg upgrade.Config, opts ...func(*service.AcquireTrackAudioService)) *upgrade.Runner {
	ledger := catalogbridge.NewTrackLedger(w.tracks)
	opts = append([]func(*service.AcquireTrackAudioService){service.WithAcquireTempWorkspace(tempfs.New())}, opts...)
	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(w.source), w.audio, opts...)
	return upgrade.NewRunner(ledger, svc, w.upgrades, cfg)
}

func (w *world) committed() domain.Track { return w.tracks.rows[rowKey(w.trackId, w.userId)] }

func (w *world) committedGrade(t *testing.T) grade.Grade {
	t.Helper()
	var stored struct {
		Grade grade.Grade `json:"grade"`
	}
	if err := json.Unmarshal(w.committed().EvidenceJSON, &stored); err != nil {
		t.Fatalf("committed evidence: %v", err)
	}
	return stored.Grade
}

func TestRunner_ALowerGradeTrackGetsTheBetterFile(t *testing.T) {
	w := seed(t, grade.F)

	if err := w.runner(upgrade.Config{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	row := w.committed()
	if row.AudioRef == nil || *row.AudioRef == originalRef || w.audio.objects[*row.AudioRef] != "new bytes" {
		t.Fatalf("audio_ref = %v, want a new ref serving the better file", row.AudioRef)
	}
	if _, stillThere := w.audio.objects[originalRef]; stillThere {
		t.Error("the superseded file must be deleted once the better one is committed")
	}
	if got := w.committedGrade(t); !got.Beats(grade.F) {
		t.Errorf("stored grade = %q, want the new file's grade above F", got)
	}
	if len(w.upgrades.attempts) != 0 || len(w.upgrades.gaveUp) != 0 {
		t.Errorf("a successful swap recorded attempts %v / give-ups %v, want none", w.upgrades.attempts, w.upgrades.gaveUp)
	}
}

func TestRunner_AnEqualGradeKeepsTheOldFileAndRejectsTheTriedSource(t *testing.T) {
	w := seed(t, grade.D)

	if err := w.runner(upgrade.Config{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); !errors.Is(err, upgrade.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v, want a kept file to be a settled job", err)
	}

	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the old ref %q kept", row.AudioRef, originalRef)
	}
	if len(w.audio.objects) != 1 || w.audio.objects[originalRef] != "original bytes" {
		t.Errorf("objects = %v, want only the untouched original: the new file is discarded", w.audio.objects)
	}
	want := []ports.UpgradeAttempt{{RejectedSourceKey: "youtube:freshAAAAAA", CountsAsTry: true}}
	if fmt.Sprint(w.upgrades.attempts) != fmt.Sprint(want) {
		t.Errorf("attempts = %v, want %v: the tried source joins the rejected keys", w.upgrades.attempts, want)
	}
}

func TestRunner_ALowerGradeKeepsTheOldFile(t *testing.T) {
	w := seed(t, grade.B)

	if err := w.runner(upgrade.Config{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); !errors.Is(err, upgrade.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want the old file alone", row.AudioRef, w.audio.objects)
	}
	if got := w.committedGrade(t); got != grade.B {
		t.Errorf("stored grade = %q, want B untouched", got)
	}
}

func TestRunner_AUserUploadIsNeverReplaced(t *testing.T) {
	w := seed(t, grade.F)
	w.asUserUpload()

	if err := w.runner(upgrade.Config{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0 for a track the user uploaded", w.source.finds)
	}
	if row := w.committed(); row.AudioRef == nil || *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want the user's file untouched", row.AudioRef, w.audio.objects)
	}
}

func TestRunner_AGradeATrackIsLeftAlone(t *testing.T) {
	w := seed(t, grade.A)

	if err := w.runner(upgrade.Config{MaxTries: 3}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0: nothing grades above A", w.source.finds)
	}
}

func TestRunner_GivesUpAfterTheMaximumTriesWithNothingBetter(t *testing.T) {
	w := seed(t, grade.D)
	runner := w.runner(upgrade.Config{MaxTries: 3})

	for try := 1; try <= 3; try++ {
		if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); !errors.Is(err, upgrade.ErrKept) {
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

func TestRunner_ADryRunReportsTheOutcomeWithoutSwappingOrCounting(t *testing.T) {
	w := seed(t, grade.F)

	if err := w.runner(upgrade.Config{MaxTries: 1, DryRun: true}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); !errors.Is(err, upgrade.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if row := w.committed(); *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want a dry run to leave the old file alone", row.AudioRef, w.audio.objects)
	}
	if fmt.Sprint(w.upgrades.attempts) != fmt.Sprint([]ports.UpgradeAttempt{{}}) || len(w.upgrades.gaveUp) != 0 {
		t.Errorf("attempts = %v, give-ups = %v, want one uncounted attempt and no give-up", w.upgrades.attempts, w.upgrades.gaveUp)
	}
}

func TestRunner_ASwapThatFailsToCommitKeepsTheOldFileAndCountsNoTry(t *testing.T) {
	w := seed(t, grade.F)
	w.tracks.updateErr = errors.New("transient db error")

	err := w.runner(upgrade.Config{MaxTries: 1}).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if err == nil {
		t.Fatal("ExecuteUpgrade = nil, want the failed commit reported")
	}
	if w.audio.objects[originalRef] != "original bytes" || len(w.audio.objects) != 1 {
		t.Errorf("objects = %v, want the original untouched and the staged file rolled back", w.audio.objects)
	}
	if row := w.committed(); row.AcquisitionStatus != domain.AcquisitionReady || *row.AudioRef != originalRef {
		t.Errorf("track = %v %v, want it ready on the original ref", row.AcquisitionStatus, row.AudioRef)
	}
	if len(w.upgrades.gaveUp) != 0 || fmt.Sprint(w.upgrades.attempts) != fmt.Sprint([]ports.UpgradeAttempt{{}}) {
		t.Errorf("attempts = %v, give-ups = %v, want an infrastructure failure to count for nothing", w.upgrades.attempts, w.upgrades.gaveUp)
	}
}

func TestRunner_PublishesNothingToTheUser(t *testing.T) {
	w := seed(t, grade.F)
	pub := &publisher{}

	if err := w.runner(upgrade.Config{MaxTries: 3}, service.WithAcquireEvents(pub)).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if pub.published != 0 {
		t.Errorf("published %d events, want a background upgrade to stay silent", pub.published)
	}
}

func TestReplace_IsNotHeldToTheRatchet(t *testing.T) {
	w := seed(t, grade.B)
	svc := service.NewAcquireTrackAudioService(catalogbridge.NewTrackLedger(w.tracks), service.NewSourceRegistry(w.source), w.audio,
		service.WithAcquireTempWorkspace(tempfs.New()))

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

func (f *failingGiveUpStore) GiveUp(context.Context, shared.TrackId, int, string) error {
	f.giveUpCalls++
	return errors.New("db down")
}

func TestRunner_AFailedGiveUpStillSettlesTheKeptFile(t *testing.T) {
	w := seed(t, grade.D)
	store := &failingGiveUpStore{}
	ledger := catalogbridge.NewTrackLedger(w.tracks)
	svc := service.NewAcquireTrackAudioService(ledger, service.NewSourceRegistry(w.source), w.audio,
		service.WithAcquireTempWorkspace(tempfs.New()))
	runner := upgrade.NewRunner(ledger, svc, store, upgrade.Config{MaxTries: 1})

	err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if !errors.Is(err, upgrade.ErrKept) {
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
	gates []upgrade.Gate
}

func (g *gateRecorder) ReplaceGated(_ context.Context, _ shared.UserId, _ *ports.TrackSnapshot, gate upgrade.Gate) (upgrade.Attempt, error) {
	g.gates = append(g.gates, gate)
	return upgrade.Attempt{}, nil
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
			w := seed(t, grade.A)
			w.storedEvidence(fmt.Sprintf(tc.evidence, grade.PipelineGeneration))
			replacer := &gateRecorder{}
			runner := upgrade.NewRunner(catalogbridge.NewTrackLedger(w.tracks), replacer, w.upgrades, upgrade.Config{MaxTries: 3, LowBitrateFloorKbps: 112})

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
	w := seed(t, grade.B)
	key := rowKey(w.trackId, w.userId)
	row := w.tracks.rows[key]
	if err := row.SetCatalogDuration(285); err != nil {
		t.Fatalf("set catalog duration: %v", err)
	}
	if err := row.SetDuration(231); err != nil {
		t.Fatalf("set stored duration: %v", err)
	}
	w.tracks.rows[key] = row
	replacer := &gateRecorder{}
	runner := upgrade.NewRunner(catalogbridge.NewTrackLedger(w.tracks), replacer, w.upgrades, upgrade.Config{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 1 || replacer.gates[0].CatalogSeconds != 285 || replacer.gates[0].StoredSeconds != 231 {
		t.Errorf("gates = %+v, want one carrying catalog 285 s and stored 231 s", replacer.gates)
	}
}

func TestRunner_ALowBitrateGateAlsoCarriesTheCatalogAndStoredLengths(t *testing.T) {
	w := seed(t, grade.A)
	w.storedEvidence(fmt.Sprintf(`{"grade":"A","source_format_id":"18","source_kbps":96,"in_place_generation":%d}`, grade.PipelineGeneration))
	key := rowKey(w.trackId, w.userId)
	row := w.tracks.rows[key]
	if err := row.SetCatalogDuration(195); err != nil {
		t.Fatalf("set catalog duration: %v", err)
	}
	if err := row.SetDuration(292); err != nil {
		t.Fatalf("set stored duration: %v", err)
	}
	w.tracks.rows[key] = row
	replacer := &gateRecorder{}
	runner := upgrade.NewRunner(catalogbridge.NewTrackLedger(w.tracks), replacer, w.upgrades, upgrade.Config{MaxTries: 3, LowBitrateFloorKbps: 112})

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
	w := seed(t, grade.B)
	replacer := &gateRecorder{}
	runner := upgrade.NewRunner(catalogbridge.NewTrackLedger(w.tracks), replacer, w.upgrades, upgrade.Config{MaxTries: 3})

	if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(replacer.gates) != 1 || replacer.gates[0].CatalogSeconds != 0 || replacer.gates[0].StoredSeconds != 0 {
		t.Errorf("gates = %+v, want one with catalog and stored lengths 0", replacer.gates)
	}
}
