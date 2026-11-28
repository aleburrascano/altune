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

	t.Run("state belongs to its own track", func(t *testing.T) {
		repo := newRepo(t)
		mine, other := repo.AddTrack(t), repo.AddTrack(t)

		settle(t, repo, other, []string{"yt:a"}, domain.KeepEvidence)

		if got := state(t, repo, mine); len(got.RejectedSourceKeys) != 0 {
			t.Errorf("State for another track = %+v, want none", got)
		}
	})
}
