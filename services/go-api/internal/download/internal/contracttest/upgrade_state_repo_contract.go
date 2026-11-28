package contracttest

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

type UpgradeStateRepoUnderTest interface {
	ports.UpgradeState
	ports.InPlaceState
	NoteAttempt(ctx context.Context, trackID ids.TrackId, attempt domain.UpgradeAttempt) (int, error)
	GiveUp(ctx context.Context, trackID ids.TrackId, generation int, reason string) error
	AddTrack(t *testing.T) ids.TrackId
	SeedEvidence(t *testing.T, trackID ids.TrackId, evidence json.RawMessage)
}

func UpgradeStateRepoContract(t *testing.T, newRepo func(t *testing.T) UpgradeStateRepoUnderTest) {
	t.Helper()
	ctx := context.Background()
	state := func(t *testing.T, repo UpgradeStateRepoUnderTest, trackID ids.TrackId) domain.UpgradeStateSnapshot {
		t.Helper()
		got, err := repo.State(ctx, trackID)
		if err != nil {
			t.Fatalf("State: %v", err)
		}
		return got
	}
	settle := func(t *testing.T, repo UpgradeStateRepoUnderTest, trackID ids.TrackId, keys []string, fate domain.EvidenceFate) {
		t.Helper()
		if err := repo.SettleAcquisition(ctx, trackID, 0, keys, fate); err != nil {
			t.Fatalf("SettleAcquisition: %v", err)
		}
	}
	evidenceKeys := func(t *testing.T, snapshot domain.UpgradeStateSnapshot) map[string]json.RawMessage {
		t.Helper()
		keys := map[string]json.RawMessage{}
		if len(snapshot.Evidence) == 0 {
			return keys
		}
		if err := json.Unmarshal(snapshot.Evidence, &keys); err != nil {
			t.Fatalf("evidence %s is not a JSON object: %v", snapshot.Evidence, err)
		}
		return keys
	}

	t.Run("a track nothing was settled for has no state", func(t *testing.T) {
		repo := newRepo(t)

		got := state(t, repo, repo.AddTrack(t))

		if len(got.RejectedSourceKeys) != 0 || len(evidenceKeys(t, got)) != 0 {
			t.Errorf("State = %+v, want no rejected keys and no evidence", got)
		}
	})

	t.Run("settling keeps the rejected source keys in order, without empties or repeats", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)

		settle(t, repo, trackID, []string{"yt:a", "", "yt:b", "yt:a"}, domain.KeepEvidence)
		settle(t, repo, trackID, []string{"yt:c", "yt:b"}, domain.KeepEvidence)

		if got, want := state(t, repo, trackID).RejectedSourceKeys, []string{"yt:a", "yt:b", "yt:c"}; !reflect.DeepEqual(got, want) {
			t.Errorf("RejectedSourceKeys = %v, want %v", got, want)
		}
	})

	t.Run("settling keeps only the newest rejected source keys", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)
		var keys []string
		for i := range domain.MaxRejectedSourceKeys + 5 {
			keys = append(keys, fmt.Sprintf("yt:%02d", i))
		}

		settle(t, repo, trackID, keys, domain.KeepEvidence)

		if got, want := state(t, repo, trackID).RejectedSourceKeys, keys[5:]; !reflect.DeepEqual(got, want) {
			t.Errorf("RejectedSourceKeys = %v, want the newest %d: %v", got, domain.MaxRejectedSourceKeys, want)
		}
	})

	t.Run("settling with the evidence kept leaves the evidence alone", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)
		repo.SeedEvidence(t, trackID, json.RawMessage(`{"upgrade_tries":2}`))

		settle(t, repo, trackID, []string{"yt:a"}, domain.KeepEvidence)

		if keys := evidenceKeys(t, state(t, repo, trackID)); string(keys["upgrade_tries"]) != "2" {
			t.Errorf("evidence = %v, want upgrade_tries kept at 2", keys)
		}
	})

	t.Run("settling with the evidence reset empties it and keeps the rejected keys", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)
		repo.SeedEvidence(t, trackID, json.RawMessage(`{"upgrade_tries":2}`))

		settle(t, repo, trackID, []string{"yt:a"}, domain.ResetEvidence)

		got := state(t, repo, trackID)
		if len(evidenceKeys(t, got)) != 0 || !reflect.DeepEqual(got.RejectedSourceKeys, []string{"yt:a"}) {
			t.Errorf("State = %+v, want no evidence and the rejected key kept", got)
		}
	})

	t.Run("an attempt that counts raises the tries and stamps when it was tried", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)

		first, err := repo.NoteAttempt(ctx, trackID, domain.UpgradeAttempt{CountsAsTry: true})
		if err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}
		uncounted, err := repo.NoteAttempt(ctx, trackID, domain.UpgradeAttempt{})
		if err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}
		second, err := repo.NoteAttempt(ctx, trackID, domain.UpgradeAttempt{CountsAsTry: true})
		if err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}

		if first != 1 || uncounted != 1 || second != 2 {
			t.Errorf("tries = %d, %d, %d, want 1, 1, 2: only an attempt that counts raises them", first, uncounted, second)
		}
		keys := evidenceKeys(t, state(t, repo, trackID))
		if string(keys["upgrade_tries"]) != "2" || len(keys["upgrade_tried_at"]) == 0 {
			t.Errorf("evidence = %v, want upgrade_tries 2 and an upgrade_tried_at", keys)
		}
	})

	t.Run("an attempt remembers the rejected source once and ignores an empty one", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)

		for _, key := range []string{"yt:a", "", "yt:a", "yt:b"} {
			if _, err := repo.NoteAttempt(ctx, trackID, domain.UpgradeAttempt{RejectedSourceKey: key}); err != nil {
				t.Fatalf("NoteAttempt %q: %v", key, err)
			}
		}

		if got, want := state(t, repo, trackID).RejectedSourceKeys, []string{"yt:a", "yt:b"}; !reflect.DeepEqual(got, want) {
			t.Errorf("RejectedSourceKeys = %v, want %v", got, want)
		}
	})

	t.Run("giving up writes the generation and the reason and resets the tries", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)
		if _, err := repo.NoteAttempt(ctx, trackID, domain.UpgradeAttempt{CountsAsTry: true}); err != nil {
			t.Fatalf("NoteAttempt: %v", err)
		}

		if err := repo.GiveUp(ctx, trackID, 3, "no source graded above D after 3 tries"); err != nil {
			t.Fatalf("GiveUp: %v", err)
		}

		keys := evidenceKeys(t, state(t, repo, trackID))
		if string(keys["best_available_generation"]) != "3" || string(keys["best_available_reason"]) != `"no source graded above D after 3 tries"` ||
			len(keys["best_available_at"]) == 0 || string(keys["upgrade_tries"]) != "0" {
			t.Errorf("evidence = %v, want the best-available generation, reason and time and no tries", keys)
		}
	})

	t.Run("an in-place state keeps the generation, the basis and the bit error rate, and clears the failures", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)
		if _, err := repo.NoteInPlaceFailure(ctx, trackID); err != nil {
			t.Fatalf("NoteInPlaceFailure: %v", err)
		}

		if err := repo.RecordInPlaceState(ctx, trackID, 2, "acoustid", 0.06); err != nil {
			t.Fatalf("RecordInPlaceState: %v", err)
		}

		keys := evidenceKeys(t, state(t, repo, trackID))
		if string(keys["in_place_generation"]) != "2" || string(keys["in_place_basis"]) != `"acoustid"` ||
			string(keys["in_place_bit_error_rate"]) != "0.06" || len(keys["in_place_at"]) == 0 {
			t.Errorf("evidence = %v, want the in-place generation, basis, bit error rate and time", keys)
		}
		if _, failing := keys["in_place_failures"]; failing {
			t.Errorf("evidence = %v, want the in-place failures cleared", keys)
		}
	})

	t.Run("an in-place state with no basis and no bit error rate leaves them out", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)

		if err := repo.RecordInPlaceState(ctx, trackID, 2, "", 0); err != nil {
			t.Fatalf("RecordInPlaceState: %v", err)
		}

		keys := evidenceKeys(t, state(t, repo, trackID))
		if _, ok := keys["in_place_basis"]; ok {
			t.Errorf("evidence = %v, want no in_place_basis", keys)
		}
		if _, ok := keys["in_place_bit_error_rate"]; ok {
			t.Errorf("evidence = %v, want no in_place_bit_error_rate", keys)
		}
	})

	t.Run("in-place failures are counted and stamp when the track was tried", func(t *testing.T) {
		repo := newRepo(t)
		trackID := repo.AddTrack(t)

		first, err := repo.NoteInPlaceFailure(ctx, trackID)
		if err != nil {
			t.Fatalf("NoteInPlaceFailure: %v", err)
		}
		second, err := repo.NoteInPlaceFailure(ctx, trackID)
		if err != nil {
			t.Fatalf("NoteInPlaceFailure: %v", err)
		}

		if first != 1 || second != 2 {
			t.Errorf("failures = %d, %d, want 1, 2", first, second)
		}
		if keys := evidenceKeys(t, state(t, repo, trackID)); string(keys["in_place_failures"]) != "2" || len(keys["upgrade_tried_at"]) == 0 {
			t.Errorf("evidence = %v, want in_place_failures 2 and an upgrade_tried_at", keys)
		}
	})

	t.Run("state belongs to its own track", func(t *testing.T) {
		repo := newRepo(t)
		mine, other := repo.AddTrack(t), repo.AddTrack(t)

		settle(t, repo, other, []string{"yt:a"}, domain.KeepEvidence)

		if got := state(t, repo, mine); len(got.RejectedSourceKeys) != 0 {
			t.Errorf("State for another track = %+v, want none", got)
		}
	})
}
