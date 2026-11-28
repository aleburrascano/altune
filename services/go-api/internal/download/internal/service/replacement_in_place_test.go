package service_test

import (
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

const verifyPerHour = 3600

type fixedChecker struct {
	finding service.Finding
	err     error
	calls   int
}

func (c *fixedChecker) CheckInPlace(context.Context, *domain.TrackSnapshot) (service.Finding, error) {
	c.calls++
	return c.finding, c.err
}

type recordingInPlaceStore struct {
	mu       sync.Mutex
	records  []domain.InPlaceRecord
	failures int
}

func (s *recordingInPlaceStore) NoteInPlaceFailure(context.Context, ids.TrackId) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures++
	return s.failures, nil
}

func (s *recordingInPlaceStore) RecordInPlace(_ context.Context, _ ids.TrackId, record domain.InPlaceRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return true, nil
}

var (
	replaceHardMatch = service.Finding{Verdict: "hard", Fingerprint: domain.FingerprintMatches, Basis: "acoustid", Score: 0.97}
	otherTake        = service.Finding{Verdict: "other_version", Fingerprint: domain.FingerprintContradicts, Basis: "acoustid", Score: 0.93}
	wrongSong        = service.Finding{Verdict: "different_song", Fingerprint: domain.FingerprintUnsupported, Basis: "acoustid", Score: 0.9}
	noAnswer         = service.Finding{}
	softMatch        = service.Finding{Verdict: "soft", Fingerprint: domain.FingerprintApproximates, Basis: "acoustid", Score: 0.9}
	previewHit       = service.Finding{Verdict: "preview", Fingerprint: domain.FingerprintMatches, Basis: "preview", BitErrorRate: 0.06}
	looseHit         = service.Finding{Verdict: "preview", Fingerprint: domain.FingerprintApproximates, Basis: "preview", BitErrorRate: 0.15}
)

func (w *world) storedEvidence(evidence string) {
	w.edit(func(track *memory.LedgerTrack) { track.Evidence = json.RawMessage(evidence) })
}

func (w *world) checkedRunner(cfg service.RunnerConfig, checker service.InPlaceProbe, recorder ports.InPlaceStore) *service.Runner {
	return w.runnerChecking(cfg, events.NoopPublisher(), func(*service.AcquireTrackAudioService) service.InPlace {
		inPlace, err := service.InPlaceCheckOf(checker, recorder, verifyPerHour, time.Now, clock.Sleep)
		if err != nil {
			w.t.Fatalf("InPlaceCheckOf: %v", err)
		}
		return inPlace
	})
}

func TestInPlaceCheckOf_RefusesAMissingCheckerOrRecorder(t *testing.T) {
	if _, err := service.InPlaceCheckOf(nil, &recordingInPlaceStore{}, verifyPerHour, time.Now, clock.Sleep); !errors.Is(err, service.ErrMissingInPlacePort) {
		t.Errorf("nil checker: err = %v, want ErrMissingInPlacePort", err)
	}
	if _, err := service.InPlaceCheckOf(&fixedChecker{}, nil, verifyPerHour, time.Now, clock.Sleep); !errors.Is(err, service.ErrMissingInPlacePort) {
		t.Errorf("nil recorder: err = %v, want ErrMissingInPlacePort", err)
	}
	if _, err := service.InPlaceCheckOf(&fixedChecker{}, &recordingInPlaceStore{}, verifyPerHour, nil, clock.Sleep); !errors.Is(err, service.ErrMissingInPlacePort) {
		t.Errorf("nil clock: err = %v, want ErrMissingInPlacePort", err)
	}
	if _, err := service.InPlaceCheckOf(&fixedChecker{}, &recordingInPlaceStore{}, verifyPerHour, time.Now, nil); !errors.Is(err, service.ErrMissingInPlacePort) {
		t.Errorf("nil sleeper: err = %v, want ErrMissingInPlacePort", err)
	}
}

func evidenceFor(g domain.Grade, kind domain.SourceKind) string {
	return fmt.Sprintf(`{"grade":%q,"source_kind":%q,"pipeline_generation":1}`, g, kind)
}

func TestRunner_AnArtTrackGradedCWithAMatchingFingerprintBecomesAWithNoDownload(t *testing.T) {
	w := seed(t, domain.GradeC)
	w.storedEvidence(evidenceFor(domain.GradeC, domain.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: replaceHardMatch}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})
	if err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}
	want := domain.InPlaceRecord{AudioRef: originalRef, Generation: domain.PipelineGeneration, Verdict: "hard", Basis: "acoustid", Score: 0.97, Grade: "A"}
	if len(recorder.records) != 1 || recorder.records[0] != want {
		t.Errorf("records = %+v, want exactly %+v", recorder.records, want)
	}
	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0: the file was proven in place", w.source.finds)
	}
	if row := w.committed(); *row.AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("audio_ref = %v, objects = %v, want the file untouched", row.AudioRef, w.audio.objects)
	}
}

func TestRunner_APreviewMatchProvesTheFileAsWellAsAFingerprint(t *testing.T) {
	w := seed(t, domain.GradeD)
	w.storedEvidence(evidenceFor(domain.GradeD, domain.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: previewHit}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "A" || recorder.records[0].Basis != "preview" || recorder.records[0].Verdict != "preview" || recorder.records[0].BitErrorRate != 0.06 {
		t.Errorf("records = %+v, want grade A on the preview basis, verdict preview, with the bit error rate", recorder.records)
	}
	if w.source.finds != 0 {
		t.Errorf("sources searched %d times, want 0", w.source.finds)
	}
}

func TestRunner_AMismatchRecordsTheVerdictThenFallsThroughToReacquire(t *testing.T) {
	w := seed(t, domain.GradeC)
	w.storedEvidence(evidenceFor(domain.GradeC, domain.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: otherTake}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})
	if err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}
	if len(recorder.records) != 1 || recorder.records[0].Grade != "F" || recorder.records[0].Verdict != "other_version" || !recorder.records[0].MayLower {
		t.Errorf("records = %+v, want the F verdict recorded", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: a failed check re-acquires", w.source.finds)
	}
	if row := w.committed(); *row.AudioRef == originalRef {
		t.Error("the better file should replace the one the fingerprint rejected")
	}
}

func TestRunner_AFingerprintThatNeverAnswersFallsThroughToReacquireWithoutChangingTheGrade(t *testing.T) {
	w := seed(t, domain.GradeC)
	w.storedEvidence(evidenceFor(domain.GradeC, domain.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: noAnswer}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, service.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].Generation != domain.PipelineGeneration {
		t.Errorf("records = %+v, want the generation stamped and the grade left alone", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1", w.source.finds)
	}
}

func TestRunner_AMatchOnAnUnofficialSourceIsBAndStillLooksForAnOfficialOne(t *testing.T) {
	w := seed(t, domain.GradeC)
	w.storedEvidence(evidenceFor(domain.GradeC, domain.FanUpload))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: replaceHardMatch}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, service.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "B" {
		t.Errorf("records = %+v, want B for a proven fan upload", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: B is still below A", w.source.finds)
	}
}

func TestRunner_ATrackCheckedThisGenerationIsNotCheckedAgain(t *testing.T) {
	w := seed(t, domain.GradeC)
	w.storedEvidence(fmt.Sprintf(`{"grade":"C","source_kind":"art_track","in_place_generation":%d}`, domain.PipelineGeneration))
	checker := &fixedChecker{finding: replaceHardMatch}

	if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, checker, &recordingInPlaceStore{}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, service.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if checker.calls != 0 || w.source.finds != 1 {
		t.Errorf("checks = %d, searches = %d, want 0 checks and a straight re-acquire", checker.calls, w.source.finds)
	}
}

func TestRunner_AUserUploadThatMatchesIsGradedAUserAndNeverReplaced(t *testing.T) {
	w := seed(t, domain.GradeD)
	w.asUserUpload()
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: replaceHardMatch}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "A_user" || recorder.records[0].UserUploadMismatch {
		t.Errorf("records = %+v, want grade A_user", recorder.records)
	}
	if w.source.finds != 0 || *w.committed().AudioRef != originalRef || len(w.audio.objects) != 1 {
		t.Errorf("searches = %d, objects = %v, want the user's file untouched", w.source.finds, w.audio.objects)
	}
}

func TestRunner_AUserUploadThatDoesNotMatchIsFlaggedAndNeverReplaced(t *testing.T) {
	for name, finding := range map[string]service.Finding{"another version": otherTake, "another song": wrongSong} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, domain.GradeD)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
				t.Fatalf("ExecuteUpgrade: %v", err)
			}

			if len(recorder.records) != 1 || !recorder.records[0].UserUploadMismatch || recorder.records[0].Grade != "" {
				t.Errorf("records = %+v, want the mismatch flagged and the grade left alone", recorder.records)
			}
			if w.source.finds != 0 || *w.committed().AudioRef != originalRef || len(w.audio.objects) != 1 {
				t.Errorf("searches = %d, objects = %v, want the user's file untouched", w.source.finds, w.audio.objects)
			}
		})
	}
}

func TestRunner_AUserUploadWithNoAnswerOrOnlyASoftOneIsNeitherGradedNorFlagged(t *testing.T) {
	for name, finding := range map[string]service.Finding{"no answer": noAnswer, "soft": softMatch} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, domain.GradeD)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
				t.Fatalf("ExecuteUpgrade: %v", err)
			}

			if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].UserUploadMismatch {
				t.Errorf("records = %+v, want only the generation stamped", recorder.records)
			}
			if w.source.finds != 0 {
				t.Errorf("searches = %d, want 0 for a user's file", w.source.finds)
			}
		})
	}
}

func TestRunner_ACheckThatFailsRecordsNothingAndReportsTheError(t *testing.T) {
	w := seed(t, domain.GradeC)
	w.storedEvidence(evidenceFor(domain.GradeC, domain.ArtTrack))
	recorder := &recordingInPlaceStore{}
	acoustIDDown := errors.New("acoustid unreachable")

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{err: acoustIDDown}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if !errors.Is(err, acoustIDDown) {
		t.Errorf("ExecuteUpgrade = %v, want the outage reported", err)
	}
	if len(recorder.records) != 0 || w.source.finds != 0 {
		t.Errorf("records = %+v, searches = %d, want nothing recorded and no download on a failed check", recorder.records, w.source.finds)
	}
	if recorder.failures != 1 {
		t.Errorf("failures noted = %d, want 1 so the track waits its turn and the count grows", recorder.failures)
	}
}

func TestRunner_AfterThreeFailedChecksTheTrackFallsThroughToReacquire(t *testing.T) {
	w := seed(t, domain.GradeC)
	w.storedEvidence(evidenceFor(domain.GradeC, domain.ArtTrack))
	recorder := &recordingInPlaceStore{}
	runner := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{err: errors.New("fpcalc cannot decode")}, recorder)

	for try := 1; try <= 2; try++ {
		if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err == nil {
			t.Fatalf("try %d: ExecuteUpgrade = nil, want the failed check reported", try)
		}
	}
	if w.source.finds != 0 {
		t.Fatalf("sources searched %d times before the third failure, want 0", w.source.finds)
	}
	if err := runner.ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, service.ErrKept) {
		t.Fatalf("third try: %v", err)
	}

	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1 once the check has failed three times", w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].Generation != domain.PipelineGeneration || recorder.records[0].Grade != "" {
		t.Errorf("records = %+v, want the generation stamped so the check is not repeated", recorder.records)
	}
}

func TestRunner_APreviewMatchAboveTheOfficialCutGradesBAndStillLooksForABetterSource(t *testing.T) {
	w := seed(t, domain.GradeD)
	w.storedEvidence(evidenceFor(domain.GradeD, domain.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: looseHit}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, service.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "B" || recorder.records[0].Verdict != "preview" || recorder.records[0].BitErrorRate != 0.15 {
		t.Errorf("records = %+v, want B with the preview verdict and the bit error rate kept", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: B is below A", w.source.finds)
	}
}

func judgedEvidence(g domain.Grade, inPlaceGeneration int) string {
	return fmt.Sprintf(`{"grade":%q,"source_kind":"art_track","in_place_generation":%d}`, g, inPlaceGeneration)
}

func TestRunner_ATrackGradedBestAvailableIsNeitherCheckedNorReplaced(t *testing.T) {
	w := seed(t, domain.BestAvailable)
	w.storedEvidence(judgedEvidence(domain.BestAvailable, 0))
	checker := &fixedChecker{finding: otherTake}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, checker, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || checker.calls != 0 || len(recorder.records) != 0 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, records = %+v, searches = %d, want the track left alone", err, checker.calls, recorder.records, w.source.finds)
	}
	if row := w.committed(); *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the original file", row.AudioRef)
	}
}

func TestRunner_AGradeATrackNotYetCheckedThisGenerationIsCheckedAndKeptWhenNothingStandsAgainstIt(t *testing.T) {
	w := seed(t, domain.GradeA)
	w.storedEvidence(judgedEvidence(domain.GradeA, 0))
	checker := &fixedChecker{finding: noAnswer}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, checker, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || checker.calls != 1 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, searches = %d, want one check and no re-acquire: unknown changes nothing", err, checker.calls, w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].Generation != domain.PipelineGeneration {
		t.Errorf("records = %+v, want the generation stamped and the grade left alone", recorder.records)
	}
}

func TestRunner_AGradeATrackCheckedThisGenerationIsSkipped(t *testing.T) {
	w := seed(t, domain.GradeA)
	w.storedEvidence(judgedEvidence(domain.GradeA, domain.PipelineGeneration))
	checker := &fixedChecker{finding: otherTake}

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, checker, &recordingInPlaceStore{}).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || checker.calls != 0 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, searches = %d, want the top grade skipped", err, checker.calls, w.source.finds)
	}
}

func TestRunner_AWrongLengthGradeAIsLoweredAndReacquired(t *testing.T) {
	w := seed(t, domain.GradeA)
	w.storedEvidence(judgedEvidence(domain.GradeA, 0))
	wrongLength := service.Finding{Verdict: "hard", Fingerprint: domain.FingerprintApproximates, Basis: "acoustid", Score: 0.97, Lowers: true}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: wrongLength}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil && !errors.Is(err, service.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}
	if len(recorder.records) != 1 || recorder.records[0].Grade != "B" || !recorder.records[0].MayLower {
		t.Errorf("records = %+v, want B recorded with permission to lower the stored A", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: a lowered grade is repaired by the lane", w.source.finds)
	}
}

func TestRunner_AProvenButNotLoweringFindingNeverLowersAStoredGrade(t *testing.T) {
	w := seed(t, domain.GradeA)
	w.storedEvidence(judgedEvidence(domain.GradeA, 0))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(service.RunnerConfig{MaxTries: 3}, &fixedChecker{finding: softMatch}, recorder).ExecuteUpgrade(context.Background(), domain.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || w.source.finds != 0 {
		t.Errorf("err = %v, searches = %d, want the stored A kept: weaker evidence now does not lower it", err, w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].MayLower {
		t.Errorf("records = %+v, want no permission to lower", recorder.records)
	}
}
