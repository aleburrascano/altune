package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	sharedids "altune/go-api/internal/shared/ids"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type readyTrackSpec struct {
	evidence  string
	sourceURL *string
}

func insertUpgradableTrack(t *testing.T, pool *pgxpool.Pool, spec readyTrackSpec) sharedids.TrackId {
	t.Helper()
	id := sharedids.TrackIdFromUUID(uuid.New())
	var evidence, state any
	if spec.evidence != "" {
		trackKeys, stateKeys := splitEvidence(t, spec.evidence)
		evidence, state = trackKeys, stateKeys
	}
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, audio_ref, audio_source_url, acquisition_evidence)
		 VALUES ($1, $2, 'Blinding Lights', 'The Weeknd', 'After Hours', $3, 'ready', $4, $5, $6::jsonb)`,
		id.UUID(), uuid.New(), uuid.NewString(), "u/"+id.String()+".mp3", spec.sourceURL, evidence)
	if err != nil {
		t.Fatalf("insert ready track: %v", err)
	}
	if state != nil {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO acquisition_upgrade_state (track_id, evidence) VALUES ($1, $2::jsonb)
			 ON CONFLICT (track_id) DO UPDATE SET evidence = EXCLUDED.evidence`, id.UUID(), state); err != nil {
			t.Fatalf("insert upgrade state: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, id.UUID())
	})
	return id
}

func splitEvidence(t *testing.T, raw string) (track, state string) {
	t.Helper()
	var all map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &all); err != nil {
		t.Fatalf("evidence %q is not a JSON object: %v", raw, err)
	}
	trackKeys, stateKeys := map[string]json.RawMessage{}, map[string]json.RawMessage{}
	for key, value := range all {
		if slices.Contains(ports.UpgradeEvidenceKeys, key) {
			stateKeys[key] = value
		} else {
			trackKeys[key] = value
		}
	}
	trackJSON, err := json.Marshal(trackKeys)
	if err != nil {
		t.Fatalf("encode track evidence: %v", err)
	}
	stateJSON, err := json.Marshal(stateKeys)
	if err != nil {
		t.Fatalf("encode state evidence: %v", err)
	}
	return string(trackJSON), string(stateJSON)
}

func sourceURL(id string) *string {
	url := "https://www.youtube.com/watch?v=" + id
	return &url
}

func evidenceOf(g string) string { return fmt.Sprintf(`{"grade":%q}`, g) }

func selectionOf(limit int) ports.UpgradeSelection {
	return ports.UpgradeSelection{Limit: limit, Generation: domain.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 10, Upgradable: contracttest.Rules{}.Upgradable()}
}

func candidatesAmong(t *testing.T, store *persistence.PostgresUpgradeStore, selection ports.UpgradeSelection, mine ...sharedids.TrackId) []sharedids.TrackId {
	t.Helper()
	selection.Limit = 1000
	candidates, err := store.Candidates(context.Background(), selection)
	if err != nil {
		t.Fatalf("Candidates: %v", err)
	}
	var ids []sharedids.TrackId
	for _, c := range candidates {
		if slices.Contains(mine, c.TrackID) {
			ids = append(ids, c.TrackID)
		}
	}
	return ids
}

func TestPostgresUpgradeStore_CandidatesAreWorstGradeFirstAndNeverTheGradeA(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	b := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("B"), sourceURL: sourceURL("bbbb")})
	f := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("ffff")})
	c := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("cccc")})
	d := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("dddd")})
	a := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A"), sourceURL: sourceURL("aaaa")})

	got := candidatesAmong(t, store, selectionOf(10), a, b, c, d, f)

	want := []sharedids.TrackId{f, d, c, b}
	if !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want worst first F, D, C, B and no grade A: %v", got, want)
	}
}

func TestPostgresUpgradeStore_ATrackWithNoGradeIsPickedAsGradeD(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	ungraded := insertUpgradableTrack(t, pool, readyTrackSpec{sourceURL: sourceURL("none")})

	got := candidatesAmong(t, store, selectionOf(10), ungraded)

	if !slices.Equal(got, []sharedids.TrackId{ungraded}) {
		t.Errorf("candidates = %v, want the ungraded track", got)
	}
}

func TestPostgresUpgradeStore_AUserUploadIsNeverACandidate(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	noSource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: nil})
	empty := ""
	emptySource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: &empty})
	fromSource := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("real")})

	got := candidatesAmong(t, store, selectionOf(10), noSource, emptySource, fromSource)

	if !slices.Equal(got, []sharedids.TrackId{fromSource}) {
		t.Errorf("candidates = %v, want only the track that came from a source", got)
	}
}

func uploadsIncluded() ports.UpgradeSelection {
	selection := selectionOf(10)
	selection.CheckInPlace = true
	return selection
}

func TestPostgresUpgradeStore_UnverifiedUserUploadsAreCandidatesOnlyWhenAsked(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	unverified := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D")})
	verified := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: fmt.Sprintf(`{"grade":"D","in_place_generation":%d}`, domain.PipelineGeneration)})
	graded := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A_user")})

	if got := candidatesAmong(t, store, selectionOf(10), unverified, verified, graded); len(got) != 0 {
		t.Errorf("candidates = %v, want no user upload unless uploads are asked for", got)
	}
	got := candidatesAmong(t, store, uploadsIncluded(), unverified, verified, graded)
	if !slices.Equal(got, []sharedids.TrackId{unverified}) {
		t.Errorf("candidates = %v, want only the upload not yet checked this generation", got)
	}
}

func TestPostgresUpgradeStore_AGradeAUserTrackIsNeverACandidate(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	aUser := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("A_user"), sourceURL: sourceURL("mine")})

	if got := candidatesAmong(t, store, uploadsIncluded(), aUser); len(got) != 0 {
		t.Errorf("candidates = %v, want none: A_user is the top grade", got)
	}
}

func TestPostgresUpgradeStore_NoteInPlaceFailureCountsAndMakesTheTrackWaitItsTurn(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
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

func TestPostgresUpgradeStore_AFileThatIsNotReadyIsNeverACandidate(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	failed := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("gone")})
	if _, err := pool.Exec(context.Background(), `UPDATE tracks SET acquisition_status = 'failed' WHERE id = $1`, failed.UUID()); err != nil {
		t.Fatalf("fail track: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), failed); len(got) != 0 {
		t.Errorf("candidates = %v, want none for a track with no playable file", got)
	}
}

func TestPostgresUpgradeStore_ATrackGivenUpOnWaitsForTheNextGeneration(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("stuck")})

	if err := store.GiveUp(context.Background(), id, domain.PipelineGeneration, "the extended mix exists only on a promo channel"); err != nil {
		t.Fatalf("GiveUp: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), id); len(got) != 0 {
		t.Errorf("candidates in the same generation = %v, want none", got)
	}
	next := selectionOf(10)
	next.Generation = domain.PipelineGeneration + 1
	if got := candidatesAmong(t, store, next, id); !slices.Equal(got, []sharedids.TrackId{id}) {
		t.Errorf("candidates after the generation bump = %v, want the track again", got)
	}
	var reason string
	if err := pool.QueryRow(context.Background(),
		`SELECT evidence->>'best_available_reason' FROM acquisition_upgrade_state WHERE track_id = $1`, id.UUID()).Scan(&reason); err != nil {
		t.Fatalf("read reason: %v", err)
	}
	if reason != "the extended mix exists only on a promo channel" {
		t.Errorf("best_available_reason = %q, want the written reason", reason)
	}
}

func TestPostgresUpgradeStore_ATrackTriedRecentlyWaitsBeforeItsNextTry(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	tried := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("tried")})
	fresh := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("fresh")})

	if _, err := store.NoteAttempt(context.Background(), tried, ports.UpgradeAttempt{}); err != nil {
		t.Fatalf("NoteAttempt: %v", err)
	}

	if got := candidatesAmong(t, store, selectionOf(10), tried, fresh); !slices.Equal(got, []sharedids.TrackId{fresh}) {
		t.Errorf("candidates = %v, want the untried track while the tried one waits", got)
	}
}

func TestPostgresUpgradeStore_NoCandidatesWhileTheUpgradeQueueIsFull(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	queued := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("queued")})
	waiting := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("waiting")})
	if err := persistence.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool}).Enqueue(context.Background(), queued, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	full := selectionOf(10)
	full.MaxQueued = 1

	if got := candidatesAmong(t, store, full, queued, waiting); len(got) != 0 {
		t.Errorf("candidates = %v, want none while the queue holds its maximum", got)
	}
}

func TestPostgresUpgradeStore_NoteAttemptCountsTriesAndRemembersTheRejectedSource(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
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
	if err := pool.QueryRow(ctx, `SELECT s.rejected_source_keys, t.acquisition_evidence->>'grade' FROM tracks t JOIN acquisition_upgrade_state s ON s.track_id = t.id WHERE t.id = $1`, id.UUID()).Scan(&keys, &grade); err != nil {
		t.Fatalf("read track: %v", err)
	}
	if !slices.Equal(keys, []string{"youtube:aaa", "youtube:bbb"}) {
		t.Errorf("rejected_source_keys = %v, want each source once", keys)
	}
	if grade != "D" {
		t.Errorf("grade = %q, want the stored grade kept", grade)
	}
}

func TestPostgresUpgradeStore_RejectedSourceKeysKeepTheNewestTwentyFive(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("current")})

	for i := range 27 {
		attempt := ports.UpgradeAttempt{RejectedSourceKey: fmt.Sprintf("youtube:%02d", i)}
		if _, err := store.NoteAttempt(context.Background(), id, attempt); err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}
	}

	var keys []string
	if err := pool.QueryRow(context.Background(), `SELECT rejected_source_keys FROM acquisition_upgrade_state WHERE track_id = $1`, id.UUID()).Scan(&keys); err != nil {
		t.Fatalf("read keys: %v", err)
	}
	if len(keys) != 25 || keys[0] != "youtube:02" || keys[24] != "youtube:26" {
		t.Errorf("rejected_source_keys = %v, want the newest 25 (02 to 26)", keys)
	}
}

func judgedEvidenceOf(g string, inPlaceGeneration int) string {
	return fmt.Sprintf(`{"grade":%q,"in_place_generation":%d}`, g, inPlaceGeneration)
}

func TestPostgresUpgradeStore_ATrackGradedBestAvailableIsNeverACandidate(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	bestAvailable := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("best_available"), sourceURL: sourceURL("leak")})
	unchecked := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("best_available", 0), sourceURL: sourceURL("leak2")})
	worst := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("ffff")})

	for name, selection := range map[string]ports.UpgradeSelection{"plain": selectionOf(10), "with unverified": uploadsIncluded()} {
		got := candidatesAmong(t, store, selection, bestAvailable, unchecked, worst)
		if !slices.Equal(got, []sharedids.TrackId{worst}) {
			t.Errorf("%s: candidates = %v, want only the F track: best_available is not on the ladder and sorts above F as text", name, got)
		}
	}
}

func TestPostgresUpgradeStore_AGradeATrackIsACandidateOnlyWhenNotYetJudgedThisGenerationAndAsked(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	unjudged := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("A", 0), sourceURL: sourceURL("old")})
	staleJudgement := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("A", domain.PipelineGeneration-1), sourceURL: sourceURL("stale")})
	judged := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: judgedEvidenceOf("A", domain.PipelineGeneration), sourceURL: sourceURL("fresh")})

	if got := candidatesAmong(t, store, selectionOf(10), unjudged, staleJudgement, judged); len(got) != 0 {
		t.Errorf("candidates = %v, want no grade A unless the in-place check is asked for", got)
	}
	got := candidatesAmong(t, store, uploadsIncluded(), unjudged, staleJudgement, judged)
	if len(got) != 2 || !slices.Contains(got, unjudged) || !slices.Contains(got, staleJudgement) {
		t.Errorf("candidates = %v, want the two grade A tracks the current judge has not seen, so a wrong-length A can be repaired", got)
	}
}

func TestUpgradeStore_Candidates_LowBitrateRescue(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	muxed := insertUpgradableTrack(t, pool, readyTrackSpec{
		evidence: `{"grade":"A","source_format_id":"18","source_kbps":96}`, sourceURL: sourceURL("muxd"),
	})
	audioOnly := insertUpgradableTrack(t, pool, readyTrackSpec{
		evidence: `{"grade":"A","source_format_id":"140","source_kbps":130}`, sourceURL: sourceURL("audio"),
	})

	withFloor := selectionOf(10)
	withFloor.LowBitrateFloorKbps = 112
	if got := candidatesAmong(t, store, withFloor, muxed, audioOnly); !slices.Equal(got, []sharedids.TrackId{muxed}) {
		t.Errorf("candidates at floor 112 = %v, want only the muxed grade-A file %v", got, muxed)
	}
	if got := candidatesAmong(t, store, selectionOf(10), muxed, audioOnly); len(got) != 0 {
		t.Errorf("candidates at floor 0 = %v, want none: a zero floor switches the rescue off", got)
	}
}

func TestPostgresUpgradeStore_StateOfATrackWithNoBookkeepingIsEmpty(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{sourceURL: sourceURL("fresh")})

	state, err := store.State(context.Background(), id)

	if err != nil || len(state.Evidence) != 0 || len(state.RejectedSourceKeys) != 0 {
		t.Errorf("State = %+v, %v, want empty", state, err)
	}
}

func TestPostgresUpgradeStore_SettleAcquisitionAppendsRejectedKeysOnceAndResetsOnlyWhenAsked(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","upgrade_tries":3}`, sourceURL: sourceURL("settle")})

	if err := store.SettleAcquisition(ctx, id, 0, []string{"yt:a", "yt:a", "", "yt:b"}, ports.KeepEvidence); err != nil {
		t.Fatalf("SettleAcquisition: %v", err)
	}
	state, err := store.State(ctx, id)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !slices.Equal(state.RejectedSourceKeys, []string{"yt:a", "yt:b"}) || !strings.Contains(string(state.Evidence), "upgrade_tries") {
		t.Errorf("state = %s %v, want each key once and the evidence kept", state.Evidence, state.RejectedSourceKeys)
	}

	if err := store.SettleAcquisition(ctx, id, 0, []string{"yt:b", "yt:c"}, ports.ResetEvidence); err != nil {
		t.Fatalf("SettleAcquisition reset: %v", err)
	}
	state, err = store.State(ctx, id)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !slices.Equal(state.RejectedSourceKeys, []string{"yt:a", "yt:b", "yt:c"}) || string(state.Evidence) != "{}" {
		t.Errorf("state = %s %v, want the keys kept and the upgrade evidence reset", state.Evidence, state.RejectedSourceKeys)
	}
}

func TestPostgresUpgradeStore_SettleAcquisitionWithNoRejectedKeysStillResetsTheEvidence(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C","upgrade_tries":3}`, sourceURL: sourceURL("nokeys")})

	if err := store.SettleAcquisition(ctx, id, 0, nil, ports.ResetEvidence); err != nil {
		t.Fatalf("SettleAcquisition with no keys: %v", err)
	}

	state, err := store.State(ctx, id)
	if err != nil || len(state.RejectedSourceKeys) != 0 || string(state.Evidence) != "{}" {
		t.Errorf("state = %s %v, %v, want no keys and the evidence reset", state.Evidence, state.RejectedSourceKeys, err)
	}
}

func TestPostgresUpgradeStore_SettleAcquisitionKeepsTheNewestTwentyFive(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{sourceURL: sourceURL("cap")})

	var keys []string
	for i := range 30 {
		keys = append(keys, fmt.Sprintf("youtube:%02d", i))
	}
	if err := store.SettleAcquisition(context.Background(), id, 0, keys, ports.KeepEvidence); err != nil {
		t.Fatalf("SettleAcquisition: %v", err)
	}

	state, err := store.State(context.Background(), id)
	if err != nil || len(state.RejectedSourceKeys) != 25 || state.RejectedSourceKeys[0] != "youtube:05" {
		t.Errorf("state keys = %v, %v, want the newest 25 (05 to 29)", state.RejectedSourceKeys, err)
	}
}

func TestPostgresUpgradeStore_ABookkeepingWriteForATrackThatIsGoneChangesNothing(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	gone := sharedids.TrackIdFromUUID(uuid.New())

	if err := store.GiveUp(context.Background(), gone, 1, "gone"); err != nil {
		t.Errorf("GiveUp for a missing track = %v, want nil as before", err)
	}
	var rows int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM acquisition_upgrade_state WHERE track_id = $1`, gone.UUID()).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("state rows = %d, %v, want none", rows, err)
	}
}

func TestPostgresUpgradeStore_BookkeepingWritesRacingATrackDeleteNeverDeadlock(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	ctx := context.Background()

	ops := []struct {
		name string
		run  func(id sharedids.TrackId) error
	}{
		{"note attempt", func(id sharedids.TrackId) error {
			_, err := store.NoteAttempt(ctx, id, ports.UpgradeAttempt{RejectedSourceKey: "yt:a", CountsAsTry: true})
			return err
		}},
		{"give up", func(id sharedids.TrackId) error { return store.GiveUp(ctx, id, 1, "nothing better") }},
		{"record in place", func(id sharedids.TrackId) error { return store.RecordInPlaceState(ctx, id, 1, "acoustid", 0.1) }},
		{"note in-place failure", func(id sharedids.TrackId) error { _, err := store.NoteInPlaceFailure(ctx, id); return err }},
		{"settle acquisition", func(id sharedids.TrackId) error {
			return store.SettleAcquisition(ctx, id, 0, []string{"yt:b"}, ports.ResetEvidence)
		}},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			for i := 0; i < 30; i++ {
				id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("C"), sourceURL: sourceURL("race")})
				if _, err := store.NoteInPlaceFailure(ctx, id); err != nil {
					t.Fatalf("seed the state row: %v", err)
				}

				var wg sync.WaitGroup
				var opErr, deleteErr error
				start := make(chan struct{})
				wg.Add(2)
				go func() { defer wg.Done(); <-start; opErr = op.run(id) }()
				go func() {
					defer wg.Done()
					<-start
					_, deleteErr = pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, id.UUID())
				}()
				close(start)
				wg.Wait()

				for _, err := range []error{opErr, deleteErr} {
					var pgErr *pgconn.PgError
					if errors.As(err, &pgErr) && pgErr.Code == "40P01" {
						t.Fatalf("round %d: %s raced a track delete into a deadlock: %v", i, op.name, err)
					}
				}
				if deleteErr != nil {
					t.Fatalf("round %d: delete = %v", i, deleteErr)
				}
			}
		})
	}
}

func TestPostgresUpgradeStore_SettleAcquisitionRefusesAStaleFenceAndWritesNothing(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)
	ctx := context.Background()

	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: `{"grade":"C"}`, sourceURL: sourceURL("fence")})
	if _, err := pool.Exec(ctx, `UPDATE acquisition_jobs SET attempts = 5 WHERE track_id = $1`, id.UUID()); err != nil {
		t.Fatalf("hold the job at attempt 5: %v", err)
	}

	err := store.SettleAcquisition(ctx, id, 4, []string{"yt:stale"}, ports.ResetEvidence)
	if !errors.Is(err, domain.ErrLeaseLost) {
		t.Fatalf("SettleAcquisition with fence 4 = %v, want ErrLeaseLost", err)
	}
	if _, keys := upgradeStateOf(t, pool, id); len(keys) != 0 {
		t.Errorf("rejected keys after a refused settle = %v, want none", keys)
	}

	if err := store.SettleAcquisition(ctx, id, 5, []string{"yt:fresh"}, ports.KeepEvidence); err != nil {
		t.Fatalf("SettleAcquisition with the held fence = %v", err)
	}
	if _, keys := upgradeStateOf(t, pool, id); !slices.Equal(keys, []string{"yt:fresh"}) {
		t.Errorf("rejected keys = %v, want [yt:fresh]", keys)
	}
}

func insertRow(t *testing.T, pool *pgxpool.Pool, row domain.UpgradeRow) sharedids.TrackId {
	t.Helper()
	ctx := context.Background()
	id := sharedids.TrackIdFromUUID(uuid.New())
	status, audioRef, url := "pending", any(nil), any(nil)
	if row.Ready {
		status = "ready"
	}
	if row.HasAudio {
		audioRef = "u/" + id.String() + ".mp3"
	}
	if row.SourceURL != "" {
		url = row.SourceURL
	}
	trackEvidence := map[string]any{}
	if row.Grade != "" {
		trackEvidence["grade"] = string(row.Grade)
	}
	if row.SourceFormatID != "" {
		trackEvidence["source_format_id"] = row.SourceFormatID
	}
	if row.SourceKbps != 0 {
		trackEvidence["source_kbps"] = row.SourceKbps
	}
	stateEvidence := map[string]any{}
	if row.InPlaceGeneration != 0 {
		stateEvidence["in_place_generation"] = row.InPlaceGeneration
	}
	if row.BestAvailableGeneration != 0 {
		stateEvidence["best_available_generation"] = row.BestAvailableGeneration
	}
	if row.UpgradeTriedAt != nil {
		stateEvidence["upgrade_tried_at"] = row.UpgradeTriedAt.UTC().Format(time.RFC3339)
	}
	trackJSON, err := json.Marshal(trackEvidence)
	if err != nil {
		t.Fatalf("encode track evidence: %v", err)
	}
	stateJSON, err := json.Marshal(stateEvidence)
	if err != nil {
		t.Fatalf("encode state evidence: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, audio_ref, audio_source_url, acquisition_evidence)
		 VALUES ($1, $2, 'Blinding Lights', 'The Weeknd', 'After Hours', $3, $4, $5, $6, $7::jsonb)`,
		id.UUID(), uuid.New(), uuid.NewString(), status, audioRef, url, string(trackJSON)); err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, id.UUID()) })
	if _, err := pool.Exec(ctx,
		`INSERT INTO acquisition_upgrade_state (track_id, evidence) VALUES ($1, $2::jsonb)`, id.UUID(), string(stateJSON)); err != nil {
		t.Fatalf("insert upgrade state: %v", err)
	}
	if row.JobQueued {
		if _, err := pool.Exec(ctx,
			`INSERT INTO acquisition_jobs (track_id, job_kind, available_at) VALUES ($1, 'acquire', now())
			 ON CONFLICT (track_id) DO UPDATE SET available_at = now()`, id.UUID()); err != nil {
			t.Fatalf("insert queued job: %v", err)
		}
	}
	return id
}

func eligibleBaseline() domain.UpgradeRow {
	return domain.UpgradeRow{Ready: true, HasAudio: true, SourceURL: "https://www.youtube.com/watch?v=base", Grade: domain.C}
}

func rowsAroundTheBaseline() map[string]domain.UpgradeRow {
	long := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-time.Minute)
	variants := map[string]func(*domain.UpgradeRow){
		"baseline":               func(*domain.UpgradeRow) {},
		"not ready":              func(r *domain.UpgradeRow) { r.Ready = false },
		"no audio":               func(r *domain.UpgradeRow) { r.HasAudio = false },
		"no source url":          func(r *domain.UpgradeRow) { r.SourceURL = "" },
		"no source url in place": func(r *domain.UpgradeRow) { r.SourceURL = ""; r.InPlaceGeneration = 1 },
		"job queued":             func(r *domain.UpgradeRow) { r.JobQueued = true },
		"ungraded":               func(r *domain.UpgradeRow) { r.Grade = "" },
		"grade F":                func(r *domain.UpgradeRow) { r.Grade = domain.F },
		"grade B":                func(r *domain.UpgradeRow) { r.Grade = domain.B },
		"grade A":                func(r *domain.UpgradeRow) { r.Grade = domain.A },
		"grade A judged": func(r *domain.UpgradeRow) {
			r.Grade = domain.A
			r.InPlaceGeneration = domain.PipelineGeneration
		},
		"grade A muxed":            func(r *domain.UpgradeRow) { r.Grade = domain.A; r.SourceFormatID = "18"; r.SourceKbps = 96 },
		"grade A muxed high kbps":  func(r *domain.UpgradeRow) { r.Grade = domain.A; r.SourceFormatID = "18"; r.SourceKbps = 130 },
		"no source url judged now": func(r *domain.UpgradeRow) { r.SourceURL = ""; r.InPlaceGeneration = domain.PipelineGeneration },
		"grade A low kbps":         func(r *domain.UpgradeRow) { r.Grade = domain.A; r.SourceFormatID = "140"; r.SourceKbps = 64 },
		"grade A at the floor":     func(r *domain.UpgradeRow) { r.Grade = domain.A; r.SourceFormatID = "140"; r.SourceKbps = 112 },
		"grade A high kbps":        func(r *domain.UpgradeRow) { r.Grade = domain.A; r.SourceFormatID = "140"; r.SourceKbps = 130 },
		"grade A unknown kbps":     func(r *domain.UpgradeRow) { r.Grade = domain.A; r.SourceFormatID = "140" },
		"grade A user":             func(r *domain.UpgradeRow) { r.Grade = domain.AUser },
		"grade A user in place":    func(r *domain.UpgradeRow) { r.Grade = domain.AUser; r.SourceURL = ""; r.InPlaceGeneration = 1 },
		"best available grade":     func(r *domain.UpgradeRow) { r.Grade = domain.BestAvailable },
		"given up this generation": func(r *domain.UpgradeRow) { r.BestAvailableGeneration = domain.PipelineGeneration },
		"given up last generation": func(r *domain.UpgradeRow) { r.BestAvailableGeneration = domain.PipelineGeneration - 1 },
		"tried a day ago":          func(r *domain.UpgradeRow) { tried := long; r.UpgradeTriedAt = &tried },
		"tried a minute ago":       func(r *domain.UpgradeRow) { tried := recent; r.UpgradeTriedAt = &tried },
	}
	rows := make(map[string]domain.UpgradeRow, len(variants))
	for name, mutate := range variants {
		row := eligibleBaseline()
		mutate(&row)
		rows[name] = row
	}
	return rows
}

func TestPostgresUpgradeStore_CandidatesAgreeWithTheGoSpecification(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	rows := rowsAroundTheBaseline()
	idOf := make(map[string]sharedids.TrackId, len(rows))
	for name, row := range rows {
		idOf[name] = insertRow(t, pool, row)
	}
	mine := make([]sharedids.TrackId, 0, len(idOf))
	for _, id := range idOf {
		mine = append(mine, id)
	}

	selections := map[string]ports.UpgradeSelection{
		"defaults":           {Upgradable: contracttest.Rules{}.Upgradable(), Generation: domain.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 1_000_000},
		"in place and floor": {Upgradable: contracttest.Rules{}.Upgradable(), Generation: domain.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 1_000_000, CheckInPlace: true, LowBitrateFloorKbps: 112},
		"floor only":         {Upgradable: contracttest.Rules{}.Upgradable(), Generation: domain.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 1_000_000, LowBitrateFloorKbps: 112},
		"long retry wait":    {Upgradable: contracttest.Rules{}.Upgradable(), Generation: domain.PipelineGeneration, RetryAfter: 72 * time.Hour, MaxQueued: 1_000_000, CheckInPlace: true},
		"no queue room":      {Upgradable: contracttest.Rules{}.Upgradable(), Generation: domain.PipelineGeneration, RetryAfter: time.Hour, MaxQueued: 0, CheckInPlace: true},
	}
	for selectionName, selection := range selections {
		t.Run(selectionName, func(t *testing.T) {
			rules := domain.UpgradeRules{
				Generation: selection.Generation, RetryAfter: selection.RetryAfter, MaxQueued: selection.MaxQueued,
				CheckInPlace: selection.CheckInPlace, LowBitrateFloorKbps: selection.LowBitrateFloorKbps, Now: time.Now(),
			}
			inSQL := make(map[sharedids.TrackId]bool)
			for _, id := range candidatesAmong(t, store, selection, mine...) {
				inSQL[id] = true
			}
			for name, row := range rows {
				if want := domain.UpgradeEligible(row, rules); inSQL[idOf[name]] != want {
					t.Errorf("%s: SQL selects = %v, Go specification = %v", name, inSQL[idOf[name]], want)
				}
			}
		})
	}
}

func TestPostgresUpgradeStore_CandidatesFollowTheGradeOrderOfTheGradeList(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresUpgradeStore(pool)

	var want, mine []sharedids.TrackId
	for _, grade := range domain.WorstFirst() {
		row := eligibleBaseline()
		row.Grade = grade
		id := insertRow(t, pool, row)
		mine = append(mine, id)
		if domain.UpgradeEligible(row, domain.UpgradeRules{Generation: domain.PipelineGeneration, MaxQueued: 1, Now: time.Now()}) {
			want = append(want, id)
		}
	}

	if got := candidatesAmong(t, store, selectionOf(10), mine...); !slices.Equal(got, want) {
		t.Errorf("candidates = %v, want the eligible grades in the list's worst-first order %v", got, want)
	}
}
