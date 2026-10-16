package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type readyTrackSpec struct {
	evidence  string
	sourceURL *string
}

func insertUpgradableTrack(t *testing.T, pool *pgxpool.Pool, spec readyTrackSpec) shared.TrackId {
	t.Helper()
	id := shared.TrackIdFromUUID(uuid.New())
	var evidence any
	if spec.evidence != "" {
		evidence = spec.evidence
	}
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, audio_ref, audio_source_url, acquisition_evidence)
		 VALUES ($1, $2, 'Blinding Lights', 'The Weeknd', 'After Hours', $3, 'ready', $4, $5, $6::jsonb)`,
		id.UUID(), uuid.New(), uuid.NewString(), "u/"+id.String()+".mp3", spec.sourceURL, evidence)
	if err != nil {
		t.Fatalf("insert ready track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, id.UUID())
	})
	return id
}

func sourceURL(id string) *string {
	url := "https://www.youtube.com/watch?v=" + id
	return &url
}

func evidenceOf(g string) string { return fmt.Sprintf(`{"grade":%q}`, g) }

func selectionOf(limit int) ports.UpgradeSelection {
	return ports.UpgradeSelection{Limit: limit, Generation: grade.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 10}
}

func candidatesAmong(t *testing.T, store *PgxUpgradeStore, selection ports.UpgradeSelection, mine ...shared.TrackId) []shared.TrackId {
	t.Helper()
	selection.Limit = 1000
	candidates, err := store.Candidates(context.Background(), selection)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	var ids []shared.TrackId
	for _, c := range candidates {
		if slices.Contains(mine, c.TrackID) {
			ids = append(ids, c.TrackID)
		}
	}
	return ids
}

func TestPgxUpgradeStore_CandidatesAreWorstGradeFirstAndNeverTheGradeA(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	b := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("B"), sourceURL: sourceURL("bbbb")})
	f := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("ffff")})
	c := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("cccc")})
	d := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("dddd")})
	a := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A"), sourceURL: sourceURL("aaaa")})

	got := candidatesAmong(t, store, selectionOf(10), a, b, c, d, f)

	want := []shared.TrackId{f, d, c, b}
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want worst first F, D, C, B and no grade A: %v", got, want)
	}
}

func TestPgxUpgradeStore_ATrackWithNoGradeIsPickedAsGradeD(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	ungraded := insertUpgradableTrack(t, pool, readyTrackSpec{sourceURL: sourceURL("none")})

	got := candidatesAmong(t, store, selectionOf(10), ungraded)

	if !slices.Equal(got, []shared.TrackId{ungraded}) {
		t.Errorf("candidates = %v, want the ungraded track", got)
	}
}

func TestPgxUpgradeStore_AUserUploadIsNeverACandidate(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	noSource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: nil})
	empty := ""
	emptySource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: &empty})
	fromSource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("real")})

	got := candidatesAmong(t, store, selectionOf(10), noSource, emptySource, fromSource)

	if !slices.Equal(got, []shared.TrackId{fromSource}) {
		t.Errorf("candidates = %v, want only the track that came from a source", got)
	}
}

func uploadsIncluded() ports.UpgradeSelection {
	selection := selectionOf(10)
	selection.IncludeUnverifiedUploads = true
	return selection
}

func TestPgxUpgradeStore_UnverifiedUserUploadsAreCandidatesOnlyWhenAsked(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	unverified := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D")})
	verified := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: fmt.Sprintf(`{"grade":"D","in_place_generation":%d}`, grade.PipelineGeneration)})
	graded := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A_user")})

	if got := candidatesAmong(t, store, selectionOf(10), unverified, verified, graded); len(got) != 0 {
		t.Errorf("candidates = %v, want no user upload unless uploads are asked for", got)
	}
	got := candidatesAmong(t, store, uploadsIncluded(), unverified, verified, graded)
	if !slices.Equal(got, []shared.TrackId{unverified}) {
		t.Errorf("candidates = %v, want only the upload not yet checked this generation", got)
	}
}

func TestPgxUpgradeStore_AGradeAUserTrackIsNeverACandidate(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	aUser := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A_user"), sourceURL: sourceURL("mine")})

	if got := candidatesAmong(t, store, uploadsIncluded(), aUser); len(got) != 0 {
		t.Errorf("candidates = %v, want none: A_user is the top grade", got)
	}
}

type recordedEvidence struct {
	Grade      string
	Verdict    string
	Basis      string
	Score      float64
	Generation int
	Mismatch   bool
	Channel    string
}

func readEvidence(t *testing.T, pool *pgxpool.Pool, id shared.TrackId) recordedEvidence {
	t.Helper()
	var evidence recordedEvidence
	err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(acquisition_evidence->>'grade', ''),
			COALESCE(acquisition_evidence->>'verdict', ''),
			COALESCE(acquisition_evidence->>'in_place_basis', ''),
			COALESCE((acquisition_evidence->>'acoustid_score')::float8, 0),
			COALESCE((acquisition_evidence->>'in_place_generation')::int, 0),
			COALESCE((acquisition_evidence->>'user_upload_mismatch')::boolean, false),
			COALESCE(acquisition_evidence->>'channel', '')
		FROM tracks WHERE id = $1`, id.UUID()).
		Scan(&evidence.Grade, &evidence.Verdict, &evidence.Basis, &evidence.Score, &evidence.Generation, &evidence.Mismatch, &evidence.Channel)
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	return evidence
}

func TestPgxUpgradeStore_RecordInPlaceWritesTheVerdictAndGradeAndKeepsTheRestOfTheEvidence(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","verdict":"unknown","channel":"topic"}`, sourceURL: sourceURL("art")})

	recorded, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "hard", Basis: "acoustid", Score: 0.97, Grade: "A",
	})

	if err != nil || !recorded {
		t.Fatalf("RecordInPlace = %v, %v, want it recorded", recorded, err)
	}
	want := recordedEvidence{Grade: "A", Verdict: "hard", Basis: "acoustid", Score: 0.97, Generation: 1, Channel: "topic"}
	if got := readEvidence(t, pool, id); got != want {
		t.Errorf("evidence = %+v, want %+v", got, want)
	}
}

func TestPgxUpgradeStore_RecordInPlaceWithNoGradeOrVerdictOnlyStampsTheGeneration(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","verdict":"unknown"}`, sourceURL: sourceURL("art")})

	if _, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{AudioRef: "u/" + id.String() + ".mp3", Generation: 1}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	want := recordedEvidence{Grade: "C", Verdict: "unknown", Generation: 1}
	if got := readEvidence(t, pool, id); got != want {
		t.Errorf("evidence = %+v, want %+v", got, want)
	}
}

func TestPgxUpgradeStore_RecordInPlaceFlagsAMismatchedUserUploadWithoutRegrading(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D")})

	if _, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "other_version", Basis: "acoustid", Score: 0.9, UserUploadMismatch: true,
	}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	want := recordedEvidence{Grade: "D", Verdict: "other_version", Basis: "acoustid", Score: 0.9, Generation: 1, Mismatch: true}
	if got := readEvidence(t, pool, id); got != want {
		t.Errorf("evidence = %+v, want %+v", got, want)
	}
}

func TestPgxUpgradeStore_RecordInPlaceNeverLowersTheStoredGrade(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("art")})
	ctx := context.Background()
	ref := "u/" + id.String() + ".mp3"

	for _, grade := range []string{"F", "D", "C"} {
		if _, err := store.RecordInPlace(ctx, id, ports.InPlaceRecord{AudioRef: ref, Generation: 1, Verdict: "other_version", Grade: grade}); err != nil {
			t.Fatalf("RecordInPlace %s: %v", grade, err)
		}
		if got := readEvidence(t, pool, id); got.Grade != "C" || got.Verdict != "other_version" {
			t.Errorf("after %s: evidence = %+v, want grade C kept and the verdict recorded", grade, got)
		}
	}
	if _, err := store.RecordInPlace(ctx, id, ports.InPlaceRecord{AudioRef: ref, Generation: 1, Verdict: "preview", Basis: "preview", BitErrorRate: 0.06, Grade: "A"}); err != nil {
		t.Fatalf("RecordInPlace A: %v", err)
	}
	var rate float64
	if err := pool.QueryRow(ctx, `SELECT (acquisition_evidence->>'in_place_bit_error_rate')::float8 FROM tracks WHERE id = $1`, id.UUID()).Scan(&rate); err != nil {
		t.Fatalf("read bit error rate: %v", err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "A" || got.Verdict != "preview" || rate != 0.06 {
		t.Errorf("evidence = %+v, rate = %v, want the raise to A with the preview verdict and its bit error rate", got, rate)
	}
}

func TestPgxUpgradeStore_RecordInPlaceLowersTheGradeOnlyForAContradictionThatStands(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("art")})

	recorded, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "other_version", Basis: "acoustid", Grade: "F", Contradicted: true,
	})

	if err != nil || !recorded {
		t.Fatalf("RecordInPlace = %v, %v, want it recorded", recorded, err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "F" || got.Verdict != "other_version" {
		t.Errorf("evidence = %+v, want the stored grade to tell the truth about the file: F", got)
	}
}

func TestPgxUpgradeStore_NoteInPlaceFailureCountsAndMakesTheTrackWaitItsTurn(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("art")})

	first, err := store.NoteInPlaceFailure(context.Background(), id)
	if err != nil {
		t.Fatalf("NoteInPlaceFailure: %v", err)
	}
	second, err := store.NoteInPlaceFailure(context.Background(), id)
	if err != nil {
		t.Fatalf("NoteInPlaceFailure: %v", err)
	}

	if first != 1 || second != 2 {
		t.Errorf("failures = %d, %d, want 1, 2", first, second)
	}
	if got := candidatesAmong(t, store, selectionOf(10), id); len(got) != 0 {
		t.Errorf("candidates = %v, want the track to wait out the retry delay", got)
	}
}

func TestPgxUpgradeStore_RecordInPlaceIgnoresAFileThatChangedMeanwhile(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("art")})

	recorded, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{AudioRef: "u/replaced-since.mp3", Generation: 1, Verdict: "hard", Grade: "A"})

	if err != nil || recorded {
		t.Fatalf("RecordInPlace = %v, %v, want a verdict on an old file to be dropped", recorded, err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "C" || got.Generation != 0 {
		t.Errorf("evidence = %+v, want the track untouched", got)
	}
}

func TestPgxUpgradeStore_AFileThatIsNotReadyIsNeverACandidate(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	failed := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("gone")})
	if _, err := pool.Exec(context.Background(), `UPDATE tracks SET acquisition_status = 'failed' WHERE id = $1`, failed.UUID()); err != nil {
		t.Fatalf("fail track: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), failed); len(got) != 0 {
		t.Errorf("candidates = %v, want none for a track with no playable file", got)
	}
}

func TestPgxUpgradeStore_ATrackGivenUpOnWaitsForTheNextGeneration(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("stuck")})

	if err := store.GiveUp(context.Background(), id, grade.PipelineGeneration, "the extended mix exists only on a promo channel"); err != nil {
		t.Fatalf("GiveUp: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), id); len(got) != 0 {
		t.Errorf("candidates in the same generation = %v, want none", got)
	}
	next := selectionOf(10)
	next.Generation = grade.PipelineGeneration + 1
	if got := candidatesAmong(t, store, next, id); !slices.Equal(got, []shared.TrackId{id}) {
		t.Errorf("candidates after the generation bump = %v, want the track again", got)
	}
	var reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_evidence->>'best_available_reason' FROM tracks WHERE id = $1`, id.UUID()).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != "the extended mix exists only on a promo channel" {
		t.Errorf("best_available_reason = %q, want the written reason", reason)
	}
}

func TestPgxUpgradeStore_ATrackTriedRecentlyWaitsBeforeItsNextTry(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	tried := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("tried")})
	fresh := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("fresh")})

	if _, err := store.NoteAttempt(context.Background(), tried, ports.UpgradeAttempt{}); err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), tried, fresh); !slices.Equal(got, []shared.TrackId{fresh}) {
		t.Errorf("candidates = %v, want the untried track while the tried one waits", got)
	}
}

func TestPgxUpgradeStore_NoCandidatesWhileTheUpgradeQueueIsFull(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	queued := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("queued")})
	waiting := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("waiting")})
	if err := NewPgxJobQueue(pool).Enqueue(context.Background(), queued, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	full := selectionOf(10)
	full.MaxQueued = 1

	if got := candidatesAmong(t, store, full, queued, waiting); len(got) != 0 {
		t.Errorf("candidates = %v, want none while the queue holds its maximum", got)
	}
}

func TestPgxUpgradeStore_NoteAttemptCountsTriesAndRemembersTheRejectedSource(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("current")})
	ctx := context.Background()

	first, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "youtube:aaa", CountsAsTry: true})
	if err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	second, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "youtube:aaa", CountsAsTry: true})
	if err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}
	uncounted, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "youtube:bbb"})
	if err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}

	if first != 1 || second != 2 || uncounted != 2 {
		t.Errorf("tries = %d, %d, %d, want 1, 2, 2 (an uncounted attempt leaves the count alone)", first, second, uncounted)
	}
	var keys []string
	var grade string
	if err := pool.QueryRow(ctx, `SELECT rejected_source_keys, acquisition_evidence->>'grade' FROM tracks WHERE id = $1`, id.UUID()).Scan(&keys, &grade); err != nil {
		t.Fatalf("read track: %v", err)
	}
	if !slices.Equal(keys, []string{"youtube:aaa", "youtube:bbb"}) {
		t.Errorf("rejected_source_keys = %v, want each source once", keys)
	}
	if grade != "D" {
		t.Errorf("grade = %q, want the stored grade kept", grade)
	}
}

func TestPgxUpgradeStore_RejectedSourceKeysKeepTheNewestTwentyFive(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("current")})

	for i := range 27 {
		attempt := ports.UpgradeAttempt{RejectedSourceKey: fmt.Sprintf("youtube:%02d", i)}
		if _, err := store.NoteAttempt(context.Background(), id, attempt); err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}
	}

	var keys []string
	if err := pool.QueryRow(context.Background(), `SELECT rejected_source_keys FROM tracks WHERE id = $1`, id.UUID()).Scan(&keys); err != nil {
		t.Fatalf("read keys: %v", err)
	}
	if len(keys) != 25 || keys[0] != "youtube:02" || keys[24] != "youtube:26" {
		t.Errorf("rejected_source_keys = %v, want the newest 25 (02 to 26)", keys)
	}
}

func judgedEvidenceOf(g string, inPlaceGeneration int) string {
	return fmt.Sprintf(`{"grade":%q,"in_place_generation":%d}`, g, inPlaceGeneration)
}

func TestPgxUpgradeStore_ATrackGradedBestAvailableIsNeverACandidate(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	bestAvailable := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("best_available"), sourceURL: sourceURL("leak")})
	unchecked := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("best_available", 0), sourceURL: sourceURL("leak2")})
	worst := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("ffff")})

	for name, selection := range map[string]ports.UpgradeSelection{"plain": selectionOf(10), "with unverified": uploadsIncluded()} {
		got := candidatesAmong(t, store, selection, bestAvailable, unchecked, worst)
		if !slices.Equal(got, []shared.TrackId{worst}) {
			t.Errorf("%s: candidates = %v, want only the F track: best_available is not on the ladder and sorts above F as text", name, got)
		}
	}
}

func TestPgxUpgradeStore_AGradeATrackIsACandidateOnlyWhenNotYetJudgedThisGenerationAndAsked(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)

	unjudged := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("A", 0), sourceURL: sourceURL("old")})
	staleJudgement := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("A", grade.PipelineGeneration-1), sourceURL: sourceURL("stale")})
	judged := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("A", grade.PipelineGeneration), sourceURL: sourceURL("fresh")})

	if got := candidatesAmong(t, store, selectionOf(10), unjudged, staleJudgement, judged); len(got) != 0 {
		t.Errorf("candidates = %v, want no grade A unless the in-place check is asked for", got)
	}
	got := candidatesAmong(t, store, uploadsIncluded(), unjudged, staleJudgement, judged)
	if len(got) != 2 || !slices.Contains(got, unjudged) || !slices.Contains(got, staleJudgement) {
		t.Errorf("candidates = %v, want the two grade A tracks the current judge has not seen, so a wrong-length A can be repaired", got)
	}
}

func TestPgxUpgradeStore_RecordInPlaceNeverReplacesAStoredBestAvailableGrade(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("best_available"), sourceURL: sourceURL("leak")})

	if _, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{
		AudioRef: "u/" + id.String() + ".mp3", Generation: 1, Verdict: "contradicted", Basis: "acoustid", Grade: "F", Contradicted: true,
	}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	if got := readEvidence(t, pool, id); got.Grade != "best_available" {
		t.Errorf("grade = %q, want best_available kept: it is never replaced", got.Grade)
	}
}

func TestPgxUpgradeStore_RecordInPlaceLowersAWrongLengthGradeAToB(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	store := NewPgxUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A"), sourceURL: sourceURL("art")})
	ref := "u/" + id.String() + ".mp3"

	if _, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{AudioRef: ref, Generation: 2, Verdict: "hard", Grade: "B"}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}
	if got := readEvidence(t, pool, id); got.Grade != "A" {
		t.Fatalf("grade = %q, want A kept: weaker evidence alone never lowers", got.Grade)
	}
	if _, err := store.RecordInPlace(context.Background(), id, ports.InPlaceRecord{AudioRef: ref, Generation: 2, Verdict: "hard", Grade: "B", Contradicted: true}); err != nil {
		t.Fatalf("RecordInPlace: %v", err)
	}

	if got := readEvidence(t, pool, id); got.Grade != "B" {
		t.Errorf("grade = %q, want B once the length contradicts the file", got.Grade)
	}
}
