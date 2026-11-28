package replacement_test

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/replacement"
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
	finding replacement.Finding
	err     error
	calls   int
}

func (c *fixedChecker) CheckInPlace(context.Context, *ports.TrackSnapshot) (replacement.Finding, error) {
	c.calls++
	return c.finding, c.err
}

type recordingInPlaceStore struct {
	mu       sync.Mutex
	records  []ports.InPlaceRecord
	failures int
}

func (s *recordingInPlaceStore) NoteInPlaceFailure(context.Context, ids.TrackId) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures++
	return s.failures, nil
}

func (s *recordingInPlaceStore) RecordInPlace(_ context.Context, _ ids.TrackId, record ports.InPlaceRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return true, nil
}

var (
	hardMatch  = replacement.Finding{Verdict: "hard", Fingerprint: audioquality.Matches, Basis: "acoustid", Score: 0.97}
	otherTake  = replacement.Finding{Verdict: "other_version", Fingerprint: audioquality.Contradicts, Basis: "acoustid", Score: 0.93}
	wrongSong  = replacement.Finding{Verdict: "different_song", Fingerprint: audioquality.Unsupported, Basis: "acoustid", Score: 0.9}
	noAnswer   = replacement.Finding{}
	softMatch  = replacement.Finding{Verdict: "soft", Fingerprint: audioquality.Approximates, Basis: "acoustid", Score: 0.9}
	previewHit = replacement.Finding{Verdict: "preview", Fingerprint: audioquality.Matches, Basis: "preview", BitErrorRate: 0.06}
	looseHit   = replacement.Finding{Verdict: "preview", Fingerprint: audioquality.Approximates, Basis: "preview", BitErrorRate: 0.15}
)

func (w *world) storedEvidence(evidence string) {
	w.edit(func(track *inmemoryledger.LedgerTrack) { track.Evidence = json.RawMessage(evidence) })
}

func (w *world) checkedRunner(cfg replacement.Config, checker replacement.InPlaceChecker, recorder ports.InPlaceStore) *replacement.Runner {
	return w.runnerChecking(cfg, events.NoopPublisher(), func(*service.AcquireTrackAudioService) replacement.InPlace {
		inPlace, err := replacement.InPlaceCheckOf(checker, recorder, verifyPerHour, time.Now, clock.Sleep)
		if err != nil {
			w.t.Fatalf("InPlaceCheckOf: %v", err)
		}
		return inPlace
	})
}

func TestInPlaceCheckOf_RefusesAMissingCheckerOrRecorder(t *testing.T) {
	if _, err := replacement.InPlaceCheckOf(nil, &recordingInPlaceStore{}, verifyPerHour, time.Now, clock.Sleep); !errors.Is(err, replacement.ErrMissingInPlacePort) {
		t.Errorf("nil checker: err = %v, want ErrMissingInPlacePort", err)
	}
	if _, err := replacement.InPlaceCheckOf(&fixedChecker{}, nil, verifyPerHour, time.Now, clock.Sleep); !errors.Is(err, replacement.ErrMissingInPlacePort) {
		t.Errorf("nil recorder: err = %v, want ErrMissingInPlacePort", err)
	}
	if _, err := replacement.InPlaceCheckOf(&fixedChecker{}, &recordingInPlaceStore{}, verifyPerHour, nil, clock.Sleep); !errors.Is(err, replacement.ErrMissingInPlacePort) {
		t.Errorf("nil clock: err = %v, want ErrMissingInPlacePort", err)
	}
	if _, err := replacement.InPlaceCheckOf(&fixedChecker{}, &recordingInPlaceStore{}, verifyPerHour, time.Now, nil); !errors.Is(err, replacement.ErrMissingInPlacePort) {
		t.Errorf("nil sleeper: err = %v, want ErrMissingInPlacePort", err)
	}
}

func evidenceFor(g audioquality.Grade, kind audioquality.SourceKind) string {
	return fmt.Sprintf(`{"grade":%q,"source_kind":%q,"pipeline_generation":1}`, g, kind)
}

func TestRunner_AnArtTrackGradedCWithAMatchingFingerprintBecomesAWithNoDownload(t *testing.T) {
	w := seed(t, audioquality.C)
	w.storedEvidence(evidenceFor(audioquality.C, audioquality.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})
	if err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}
	want := ports.InPlaceRecord{AudioRef: originalRef, Generation: audioquality.PipelineGeneration, Verdict: "hard", Basis: "acoustid", Score: 0.97, Grade: "A"}
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
	w := seed(t, audioquality.D)
	w.storedEvidence(evidenceFor(audioquality.D, audioquality.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: previewHit}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
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
	w := seed(t, audioquality.C)
	w.storedEvidence(evidenceFor(audioquality.C, audioquality.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: otherTake}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})
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
	w := seed(t, audioquality.C)
	w.storedEvidence(evidenceFor(audioquality.C, audioquality.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: noAnswer}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, replacement.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].Generation != audioquality.PipelineGeneration {
		t.Errorf("records = %+v, want the generation stamped and the grade left alone", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1", w.source.finds)
	}
}

func TestRunner_AMatchOnAnUnofficialSourceIsBAndStillLooksForAnOfficialOne(t *testing.T) {
	w := seed(t, audioquality.C)
	w.storedEvidence(evidenceFor(audioquality.C, audioquality.FanUpload))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, replacement.ErrKept) {
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
	w := seed(t, audioquality.C)
	w.storedEvidence(fmt.Sprintf(`{"grade":"C","source_kind":"art_track","in_place_generation":%d}`, audioquality.PipelineGeneration))
	checker := &fixedChecker{finding: hardMatch}

	if err := w.checkedRunner(replacement.Config{MaxTries: 3}, checker, &recordingInPlaceStore{}).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, replacement.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if checker.calls != 0 || w.source.finds != 1 {
		t.Errorf("checks = %d, searches = %d, want 0 checks and a straight re-acquire", checker.calls, w.source.finds)
	}
}

func TestRunner_AUserUploadThatMatchesIsGradedAUserAndNeverReplaced(t *testing.T) {
	w := seed(t, audioquality.D)
	w.asUserUpload()
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
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
	for name, finding := range map[string]replacement.Finding{"another version": otherTake, "another song": wrongSong} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, audioquality.D)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
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
	for name, finding := range map[string]replacement.Finding{"no answer": noAnswer, "soft": softMatch} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, audioquality.D)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil {
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
	w := seed(t, audioquality.C)
	w.storedEvidence(evidenceFor(audioquality.C, audioquality.ArtTrack))
	recorder := &recordingInPlaceStore{}
	acoustIDDown := errors.New("acoustid unreachable")

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{err: acoustIDDown}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

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
	w := seed(t, audioquality.C)
	w.storedEvidence(evidenceFor(audioquality.C, audioquality.ArtTrack))
	recorder := &recordingInPlaceStore{}
	runner := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{err: errors.New("fpcalc cannot decode")}, recorder)

	for try := 1; try <= 2; try++ {
		if err := runner.ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err == nil {
			t.Fatalf("try %d: ExecuteUpgrade = nil, want the failed check reported", try)
		}
	}
	if w.source.finds != 0 {
		t.Fatalf("sources searched %d times before the third failure, want 0", w.source.finds)
	}
	if err := runner.ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, replacement.ErrKept) {
		t.Fatalf("third try: %v", err)
	}

	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1 once the check has failed three times", w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].Generation != audioquality.PipelineGeneration || recorder.records[0].Grade != "" {
		t.Errorf("records = %+v, want the generation stamped so the check is not repeated", recorder.records)
	}
}

func TestRunner_APreviewMatchAboveTheOfficialCutGradesBAndStillLooksForABetterSource(t *testing.T) {
	w := seed(t, audioquality.D)
	w.storedEvidence(evidenceFor(audioquality.D, audioquality.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: looseHit}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId}); err != nil && !errors.Is(err, replacement.ErrKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "B" || recorder.records[0].Verdict != "preview" || recorder.records[0].BitErrorRate != 0.15 {
		t.Errorf("records = %+v, want B with the preview verdict and the bit error rate kept", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: B is below A", w.source.finds)
	}
}

func judgedEvidence(g audioquality.Grade, inPlaceGeneration int) string {
	return fmt.Sprintf(`{"grade":%q,"source_kind":"art_track","in_place_generation":%d}`, g, inPlaceGeneration)
}

func TestRunner_ATrackGradedBestAvailableIsNeitherCheckedNorReplaced(t *testing.T) {
	w := seed(t, audioquality.BestAvailable)
	w.storedEvidence(judgedEvidence(audioquality.BestAvailable, 0))
	checker := &fixedChecker{finding: otherTake}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, checker, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || checker.calls != 0 || len(recorder.records) != 0 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, records = %+v, searches = %d, want the track left alone", err, checker.calls, recorder.records, w.source.finds)
	}
	if row := w.committed(); *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the original file", row.AudioRef)
	}
}

func TestRunner_AGradeATrackNotYetCheckedThisGenerationIsCheckedAndKeptWhenNothingStandsAgainstIt(t *testing.T) {
	w := seed(t, audioquality.A)
	w.storedEvidence(judgedEvidence(audioquality.A, 0))
	checker := &fixedChecker{finding: noAnswer}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, checker, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || checker.calls != 1 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, searches = %d, want one check and no re-acquire: unknown changes nothing", err, checker.calls, w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].Generation != audioquality.PipelineGeneration {
		t.Errorf("records = %+v, want the generation stamped and the grade left alone", recorder.records)
	}
}

func TestRunner_AGradeATrackCheckedThisGenerationIsSkipped(t *testing.T) {
	w := seed(t, audioquality.A)
	w.storedEvidence(judgedEvidence(audioquality.A, audioquality.PipelineGeneration))
	checker := &fixedChecker{finding: otherTake}

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, checker, &recordingInPlaceStore{}).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || checker.calls != 0 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, searches = %d, want the top grade skipped", err, checker.calls, w.source.finds)
	}
}

func TestRunner_AWrongLengthGradeAIsLoweredAndReacquired(t *testing.T) {
	w := seed(t, audioquality.A)
	w.storedEvidence(judgedEvidence(audioquality.A, 0))
	wrongLength := replacement.Finding{Verdict: "hard", Fingerprint: audioquality.Approximates, Basis: "acoustid", Score: 0.97, Lowers: true}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: wrongLength}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil && !errors.Is(err, replacement.ErrKept) {
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
	w := seed(t, audioquality.A)
	w.storedEvidence(judgedEvidence(audioquality.A, 0))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(replacement.Config{MaxTries: 3}, &fixedChecker{finding: softMatch}, recorder).ExecuteUpgrade(context.Background(), ports.AcquisitionJob{UserID: w.userId, TrackID: w.trackId})

	if err != nil || w.source.finds != 0 {
		t.Errorf("err = %v, searches = %d, want the stored A kept: weaker evidence now does not lower it", err, w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].MayLower {
		t.Errorf("records = %+v, want no permission to lower", recorder.records)
	}
}
