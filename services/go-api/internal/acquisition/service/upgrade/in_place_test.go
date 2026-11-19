package upgrade_test

import (
	"altune/go-api/internal/acquisition"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

const verifyPerHour = 3600

type fixedChecker struct {
	finding acquisition.UpgradeFinding
	err     error
	calls   int
}

func (c *fixedChecker) CheckInPlace(context.Context, *acquisition.TrackSnapshot) (acquisition.UpgradeFinding, error) {
	c.calls++
	return c.finding, c.err
}

type recordingInPlaceStore struct {
	mu       sync.Mutex
	records  []acquisition.InPlaceRecord
	failures int
}

func (s *recordingInPlaceStore) NoteInPlaceFailure(context.Context, shared.TrackId) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures++
	return s.failures, nil
}

func (s *recordingInPlaceStore) RecordInPlace(_ context.Context, _ shared.TrackId, record acquisition.InPlaceRecord) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return true, nil
}

var (
	hardMatch  = acquisition.UpgradeFinding{Verdict: "hard", Fingerprint: acquisition.Matches, Basis: "acoustid", Score: 0.97}
	otherTake  = acquisition.UpgradeFinding{Verdict: "other_version", Fingerprint: acquisition.Contradicts, Basis: "acoustid", Score: 0.93}
	wrongSong  = acquisition.UpgradeFinding{Verdict: "different_song", Fingerprint: acquisition.FingerprintUnsupported, Basis: "acoustid", Score: 0.9}
	noAnswer   = acquisition.UpgradeFinding{}
	softMatch  = acquisition.UpgradeFinding{Verdict: "soft", Fingerprint: acquisition.Approximates, Basis: "acoustid", Score: 0.9}
	previewHit = acquisition.UpgradeFinding{Verdict: "preview", Fingerprint: acquisition.Matches, Basis: "preview", BitErrorRate: 0.06}
	looseHit   = acquisition.UpgradeFinding{Verdict: "preview", Fingerprint: acquisition.Approximates, Basis: "preview", BitErrorRate: 0.15}
)

func (w *world) storedEvidence(evidence string) {
	key := rowKey(w.trackId, w.userId)
	row := w.tracks.rows[key]
	row.EvidenceJSON = json.RawMessage(evidence)
	w.tracks.rows[key] = row
}

func (w *world) checkedRunner(cfg acquisition.UpgradeConfig, checker acquisition.InPlaceChecker, recorder acquisition.InPlaceStore) *acquisition.UpgradeRunner {
	return w.runner(cfg).WithInPlaceCheck(checker, recorder, verifyPerHour)
}

func evidenceFor(g acquisition.Grade, kind acquisition.SourceKind) string {
	return fmt.Sprintf(`{"grade":%q,"source_kind":%q,"pipeline_generation":1}`, g, kind)
}

func TestRunner_AnArtTrackGradedCWithAMatchingFingerprintBecomesAWithNoDownload(t *testing.T) {
	w := seed(t, acquisition.GradeC)
	w.storedEvidence(evidenceFor(acquisition.GradeC, acquisition.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)
	if err != nil {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}
	want := acquisition.InPlaceRecord{AudioRef: originalRef, Generation: acquisition.PipelineGeneration, Verdict: "hard", Basis: "acoustid", Score: 0.97, Grade: "A"}
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
	w := seed(t, acquisition.GradeD)
	w.storedEvidence(evidenceFor(acquisition.GradeD, acquisition.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: previewHit}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
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
	w := seed(t, acquisition.GradeC)
	w.storedEvidence(evidenceFor(acquisition.GradeC, acquisition.ArtTrack))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: otherTake}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)
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
	w := seed(t, acquisition.GradeC)
	w.storedEvidence(evidenceFor(acquisition.GradeC, acquisition.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: noAnswer}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, acquisition.ErrUpgradeKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].Generation != acquisition.PipelineGeneration {
		t.Errorf("records = %+v, want the generation stamped and the grade left alone", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1", w.source.finds)
	}
}

func TestRunner_AMatchOnAnUnofficialSourceIsBAndStillLooksForAnOfficialOne(t *testing.T) {
	w := seed(t, acquisition.GradeC)
	w.storedEvidence(evidenceFor(acquisition.GradeC, acquisition.FanUpload))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, acquisition.ErrUpgradeKept) {
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
	w := seed(t, acquisition.GradeC)
	w.storedEvidence(fmt.Sprintf(`{"grade":"C","source_kind":"art_track","in_place_generation":%d}`, acquisition.PipelineGeneration))
	checker := &fixedChecker{finding: hardMatch}

	if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, checker, &recordingInPlaceStore{}).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, acquisition.ErrUpgradeKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if checker.calls != 0 || w.source.finds != 1 {
		t.Errorf("checks = %d, searches = %d, want 0 checks and a straight re-acquire", checker.calls, w.source.finds)
	}
}

func TestRunner_AUserUploadThatMatchesIsGradedAUserAndNeverReplaced(t *testing.T) {
	w := seed(t, acquisition.GradeD)
	w.asUserUpload()
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: hardMatch}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
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
	for name, finding := range map[string]acquisition.UpgradeFinding{"another version": otherTake, "another song": wrongSong} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, acquisition.GradeD)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
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
	for name, finding := range map[string]acquisition.UpgradeFinding{"no answer": noAnswer, "soft": softMatch} {
		t.Run(name, func(t *testing.T) {
			w := seed(t, acquisition.GradeD)
			w.asUserUpload()
			recorder := &recordingInPlaceStore{}

			if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: finding}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil {
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
	w := seed(t, acquisition.GradeC)
	w.storedEvidence(evidenceFor(acquisition.GradeC, acquisition.ArtTrack))
	recorder := &recordingInPlaceStore{}
	acoustIDDown := errors.New("acoustid unreachable")

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{err: acoustIDDown}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

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
	w := seed(t, acquisition.GradeC)
	w.storedEvidence(evidenceFor(acquisition.GradeC, acquisition.ArtTrack))
	recorder := &recordingInPlaceStore{}
	runner := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{err: errors.New("fpcalc cannot decode")}, recorder)

	for try := 1; try <= 2; try++ {
		if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err == nil {
			t.Fatalf("try %d: ExecuteUpgrade = nil, want the failed check reported", try)
		}
	}
	if w.source.finds != 0 {
		t.Fatalf("sources searched %d times before the third failure, want 0", w.source.finds)
	}
	if err := runner.ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, acquisition.ErrUpgradeKept) {
		t.Fatalf("third try: %v", err)
	}

	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1 once the check has failed three times", w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].Generation != acquisition.PipelineGeneration || recorder.records[0].Grade != "" {
		t.Errorf("records = %+v, want the generation stamped so the check is not repeated", recorder.records)
	}
}

func TestRunner_APreviewMatchAboveTheOfficialCutGradesBAndStillLooksForABetterSource(t *testing.T) {
	w := seed(t, acquisition.GradeD)
	w.storedEvidence(evidenceFor(acquisition.GradeD, acquisition.ArtTrack))
	recorder := &recordingInPlaceStore{}

	if err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: looseHit}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId); err != nil && !errors.Is(err, acquisition.ErrUpgradeKept) {
		t.Fatalf("ExecuteUpgrade: %v", err)
	}

	if len(recorder.records) != 1 || recorder.records[0].Grade != "B" || recorder.records[0].Verdict != "preview" || recorder.records[0].BitErrorRate != 0.15 {
		t.Errorf("records = %+v, want B with the preview verdict and the bit error rate kept", recorder.records)
	}
	if w.source.finds != 1 {
		t.Errorf("sources searched %d times, want 1: B is below A", w.source.finds)
	}
}

func judgedEvidence(g acquisition.Grade, inPlaceGeneration int) string {
	return fmt.Sprintf(`{"grade":%q,"source_kind":"art_track","in_place_generation":%d}`, g, inPlaceGeneration)
}

func TestRunner_ATrackGradedBestAvailableIsNeitherCheckedNorReplaced(t *testing.T) {
	w := seed(t, acquisition.GradeBestAvailable)
	w.storedEvidence(judgedEvidence(acquisition.GradeBestAvailable, 0))
	checker := &fixedChecker{finding: otherTake}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, checker, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if err != nil || checker.calls != 0 || len(recorder.records) != 0 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, records = %+v, searches = %d, want the track left alone", err, checker.calls, recorder.records, w.source.finds)
	}
	if row := w.committed(); *row.AudioRef != originalRef {
		t.Errorf("audio_ref = %v, want the original file", row.AudioRef)
	}
}

func TestRunner_AGradeATrackNotYetCheckedThisGenerationIsCheckedAndKeptWhenNothingStandsAgainstIt(t *testing.T) {
	w := seed(t, acquisition.GradeA)
	w.storedEvidence(judgedEvidence(acquisition.GradeA, 0))
	checker := &fixedChecker{finding: noAnswer}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, checker, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if err != nil || checker.calls != 1 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, searches = %d, want one check and no re-acquire: unknown changes nothing", err, checker.calls, w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].Grade != "" || recorder.records[0].Generation != acquisition.PipelineGeneration {
		t.Errorf("records = %+v, want the generation stamped and the grade left alone", recorder.records)
	}
}

func TestRunner_AGradeATrackCheckedThisGenerationIsSkipped(t *testing.T) {
	w := seed(t, acquisition.GradeA)
	w.storedEvidence(judgedEvidence(acquisition.GradeA, acquisition.PipelineGeneration))
	checker := &fixedChecker{finding: otherTake}

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, checker, &recordingInPlaceStore{}).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if err != nil || checker.calls != 0 || w.source.finds != 0 {
		t.Errorf("err = %v, checks = %d, searches = %d, want the top grade skipped", err, checker.calls, w.source.finds)
	}
}

func TestRunner_AWrongLengthGradeAIsLoweredAndReacquired(t *testing.T) {
	w := seed(t, acquisition.GradeA)
	w.storedEvidence(judgedEvidence(acquisition.GradeA, 0))
	wrongLength := acquisition.UpgradeFinding{Verdict: "hard", Fingerprint: acquisition.Approximates, Basis: "acoustid", Score: 0.97, Lowers: true}
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: wrongLength}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if err != nil && !errors.Is(err, acquisition.ErrUpgradeKept) {
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
	w := seed(t, acquisition.GradeA)
	w.storedEvidence(judgedEvidence(acquisition.GradeA, 0))
	recorder := &recordingInPlaceStore{}

	err := w.checkedRunner(acquisition.UpgradeConfig{MaxTries: 3}, &fixedChecker{finding: softMatch}, recorder).ExecuteUpgrade(context.Background(), w.userId, w.trackId)

	if err != nil || w.source.finds != 0 {
		t.Errorf("err = %v, searches = %d, want the stored A kept: weaker evidence now does not lower it", err, w.source.finds)
	}
	if len(recorder.records) != 1 || recorder.records[0].MayLower {
		t.Errorf("records = %+v, want no permission to lower", recorder.records)
	}
}
