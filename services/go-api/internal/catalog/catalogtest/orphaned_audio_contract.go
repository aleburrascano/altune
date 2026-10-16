package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func OrphanedAudioQueueContract(t *testing.T, newQueue func(t *testing.T) (ports.OrphanedAudioQueue, func(track *domain.Track))) {
	ctx := context.Background()

	refFor := func(userId shared.UserId) string {
		return userId.String() + "/artist/album/title-" + uuid.NewString()[:8] + ".mp3"
	}

	record := func(t *testing.T, q ports.OrphanedAudioQueue, userId shared.UserId, ref string) ports.OrphanedAudio {
		t.Helper()
		orphan := ports.OrphanedAudio{AudioRef: ref, UserId: userId, TrackId: domain.NewTrackId()}
		if err := q.RecordOrphanedAudio(ctx, orphan); err != nil {
			t.Fatalf("RecordOrphanedAudio: %v", err)
		}
		return orphan
	}

	listed := func(t *testing.T, q ports.OrphanedAudioQueue, ref string) (ports.OrphanedAudio, int) {
		t.Helper()
		all, err := q.ListOrphanedAudio(ctx, 10_000)
		if err != nil {
			t.Fatalf("ListOrphanedAudio: %v", err)
		}
		var found ports.OrphanedAudio
		count := 0
		for _, o := range all {
			if o.AudioRef == ref {
				found = o
				count++
			}
		}
		return found, count
	}

	t.Run("a recorded ref is listed with its owner, track and a timestamp", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		orphan := record(t, q, userId, refFor(userId))
		got, count := listed(t, q, orphan.AudioRef)
		if count != 1 || got.UserId != userId || got.TrackId != orphan.TrackId || got.Attempts != 0 || got.RecordedAt.IsZero() {
			t.Fatalf("listed = %+v (x%d), want owner, track, zero attempts, a timestamp", got, count)
		}
	})

	t.Run("a ref recorded with a future not-before is listed only once it passes", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		held, due := refFor(userId), refFor(userId)
		for ref, notBefore := range map[string]time.Time{held: time.Now().Add(time.Hour), due: time.Now().Add(-time.Minute)} {
			orphan := ports.OrphanedAudio{AudioRef: ref, UserId: userId, TrackId: domain.NewTrackId(), NotBefore: notBefore}
			if err := q.RecordOrphanedAudio(ctx, orphan); err != nil {
				t.Fatalf("RecordOrphanedAudio: %v", err)
			}
		}
		if _, count := listed(t, q, held); count != 0 {
			t.Errorf("held ref listed %d times before its not-before, want 0", count)
		}
		if _, count := listed(t, q, due); count != 1 {
			t.Errorf("due ref listed %d times, want 1", count)
		}
	})

	t.Run("recording the same ref twice lists it once", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		ref := refFor(userId)
		record(t, q, userId, ref)
		second := record(t, q, userId, ref)
		got, count := listed(t, q, ref)
		if count != 1 {
			t.Fatalf("ref listed %d times, want 1", count)
		}
		if got.TrackId != second.TrackId {
			t.Errorf("track = %v, want the latest recorded %v", got.TrackId, second.TrackId)
		}
	})

	t.Run("list honours the limit", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		for range 3 {
			record(t, q, userId, refFor(userId))
		}
		got, err := q.ListOrphanedAudio(ctx, 2)
		if err != nil {
			t.Fatalf("ListOrphanedAudio: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("listed %d with limit 2, want 2", len(got))
		}
	})

	t.Run("resolve removes the ref", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		ref := refFor(userId)
		record(t, q, userId, ref)
		if err := q.ResolveOrphanedAudio(ctx, ref); err != nil {
			t.Fatalf("ResolveOrphanedAudio: %v", err)
		}
		if _, count := listed(t, q, ref); count != 0 {
			t.Error("resolved ref still listed")
		}
	})

	t.Run("marking an attempt raises Attempts", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		ref := refFor(userId)
		record(t, q, userId, ref)
		for want := 1; want <= 2; want++ {
			if err := q.MarkOrphanedAudioAttempt(ctx, ref, "storage down"); err != nil {
				t.Fatalf("MarkOrphanedAudioAttempt: %v", err)
			}
			if got, _ := listed(t, q, ref); got.Attempts != want {
				t.Errorf("attempts = %d, want %d", got.Attempts, want)
			}
		}
	})

	t.Run("recording again resets Attempts", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		ref := refFor(userId)
		record(t, q, userId, ref)
		if err := q.MarkOrphanedAudioAttempt(ctx, ref, "storage down"); err != nil {
			t.Fatal(err)
		}
		record(t, q, userId, ref)
		if got, _ := listed(t, q, ref); got.Attempts != 0 {
			t.Errorf("attempts after re-record = %d, want 0", got.Attempts)
		}
	})

	t.Run("marking an unknown ref is not an error and lists nothing", func(t *testing.T) {
		q, _ := newQueue(t)
		ref := refFor(shared.NewUserId(uuid.New()))
		if err := q.MarkOrphanedAudioAttempt(ctx, ref, "x"); err != nil {
			t.Fatalf("MarkOrphanedAudioAttempt: %v", err)
		}
		if _, count := listed(t, q, ref); count != 0 {
			t.Error("marking an unknown ref created an entry")
		}
	})

	t.Run("list puts attempted refs after never-attempted ones", func(t *testing.T) {
		q, _ := newQueue(t)
		userId := shared.NewUserId(uuid.New())
		stuck, fresh := refFor(userId), refFor(userId)
		record(t, q, userId, stuck)
		record(t, q, userId, fresh)
		if err := q.MarkOrphanedAudioAttempt(ctx, stuck, "storage down"); err != nil {
			t.Fatal(err)
		}
		all, err := q.ListOrphanedAudio(ctx, 10_000)
		if err != nil {
			t.Fatal(err)
		}
		pos := map[string]int{}
		for i, o := range all {
			pos[o.AudioRef] = i
		}
		if pos[fresh] > pos[stuck] {
			t.Errorf("never-attempted ref listed at %d after the attempted one at %d", pos[fresh], pos[stuck])
		}
	})

	t.Run("audio usage", func(t *testing.T) {
		q, seed := newQueue(t)
		usage := func(ref string, owner shared.UserId) ports.AudioUsage {
			t.Helper()
			got, err := q.AudioUsage(ctx, ref, owner)
			if err != nil {
				t.Fatalf("AudioUsage: %v", err)
			}
			return got
		}
		newTrack := func(userId shared.UserId, audioRef string) *domain.Track {
			t.Helper()
			track, err := domain.NewTrack(userId, "Title-"+uuid.NewString()[:8], "Artist-"+uuid.NewString()[:8], "Album")
			if err != nil {
				t.Fatalf("NewTrack: %v", err)
			}
			if audioRef != "" {
				if err := track.MarkReady(audioRef); err != nil {
					t.Fatalf("MarkReady: %v", err)
				}
			}
			return track
		}

		owner := shared.NewUserId(uuid.New())
		ref := refFor(owner)
		if got := usage(ref, owner); got != ports.AudioUnused {
			t.Errorf("unreferenced ref usage = %v, want unused", got)
		}

		other := shared.NewUserId(uuid.New())
		seed(newTrack(other, ref))
		if got := usage(ref, owner); got != ports.AudioReferenced {
			t.Errorf("ref held by a ready track usage = %v, want referenced", got)
		}

		busyOwner := shared.NewUserId(uuid.New())
		busyRef := refFor(busyOwner)
		seed(newTrack(busyOwner, ""))
		if got := usage(busyRef, busyOwner); got != ports.AudioOwnerAcquiring {
			t.Errorf("owner with a track still acquiring usage = %v, want owner acquiring", got)
		}
		if got := usage(busyRef, owner); got != ports.AudioUnused {
			t.Errorf("another user's acquiring track blocked the ref: usage = %v, want unused", got)
		}
	})
}
