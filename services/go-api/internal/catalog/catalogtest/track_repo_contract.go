package catalogtest

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type TrackRepositoryUnderTest interface {
	ports.TrackAdder
	ports.TrackGetter
	ports.TrackBatchGetter
	ports.TrackCounter
	ports.TrackLister
	ports.TrackUpdater
	ports.TrackDeleter
	ports.TrackAudioRefLookup
}

func contractTrack(t *testing.T, userId shared.UserId) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Title-"+uuid.NewString()[:8], "Artist-"+uuid.NewString()[:8], "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	return track
}

func contractAdd(t *testing.T, repo TrackRepositoryUnderTest, track *domain.Track) {
	t.Helper()
	_, created, err := repo.Add(context.Background(), track)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !created {
		t.Fatal("Add created = false, want true")
	}
}

func contractLoad(t *testing.T, repo TrackRepositoryUnderTest, id domain.TrackId, userId shared.UserId) *domain.Track {
	t.Helper()
	got, err := repo.GetByID(context.Background(), id, userId)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil, want track")
	}
	return got
}

func TrackRepositoryContract(t *testing.T, newRepo func(t *testing.T) TrackRepositoryUnderTest) {
	ctx := context.Background()

	t.Run("add then get returns the same fields", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		track := contractTrack(t, userId)
		contractAdd(t, repo, track)

		got := contractLoad(t, repo, track.ID, userId)
		if got.ID != track.ID || got.UserId != userId {
			t.Errorf("identity = %v/%v, want %v/%v", got.ID, got.UserId, track.ID, userId)
		}
		if got.Title != track.Title || got.Artist != track.Artist || got.Album != track.Album {
			t.Errorf("text = %q/%q/%q, want %q/%q/%q", got.Title, got.Artist, got.Album, track.Title, track.Artist, track.Album)
		}
		if got.AcquisitionStatus != domain.AcquisitionPending {
			t.Errorf("AcquisitionStatus = %v, want pending", got.AcquisitionStatus)
		}
		if got.DedupKey != track.DedupKey {
			t.Errorf("DedupKey = %q, want %q", got.DedupKey, track.DedupKey)
		}
	})

	t.Run("get of an unknown id returns nil", func(t *testing.T) {
		repo := newRepo(t)
		got, err := repo.GetByID(ctx, domain.NewTrackId(), shared.NewUserId(uuid.New()))
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got != nil {
			t.Errorf("GetByID = %v, want nil", got.ID)
		}
	})

	t.Run("a second add with the same dedup key returns the stored track", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		first := contractTrack(t, userId)
		contractAdd(t, repo, first)

		second, err := domain.NewTrack(userId, first.Title, first.Artist, first.Album)
		if err != nil {
			t.Fatalf("NewTrack: %v", err)
		}
		stored, created, err := repo.Add(ctx, second)
		if err != nil {
			t.Fatalf("second Add: %v", err)
		}
		if created {
			t.Error("second Add created = true, want false")
		}
		if stored == nil || stored.ID != first.ID {
			t.Errorf("second Add returned %v, want the first track %v", stored, first.ID)
		}
	})

	t.Run("another user's track is invisible", func(t *testing.T) {
		repo := newRepo(t)
		owner := shared.NewUserId(uuid.New())
		intruder := shared.NewUserId(uuid.New())
		track := contractTrack(t, owner)
		contractAdd(t, repo, track)

		got, err := repo.GetByID(ctx, track.ID, intruder)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got != nil {
			t.Error("GetByID exposed another user's track")
		}

		listed, err := repo.ListByIDs(ctx, intruder, []domain.TrackId{track.ID})
		if err != nil {
			t.Fatalf("ListByIDs: %v", err)
		}
		if len(listed) != 0 {
			t.Errorf("ListByIDs returned %d tracks for another user, want 0", len(listed))
		}

		deleted, ref, err := repo.Delete(ctx, track.ID, intruder)
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if deleted || ref != nil {
			t.Errorf("Delete = %v/%v for another user, want false/nil", deleted, ref)
		}
		contractLoad(t, repo, track.ID, owner)
	})

	t.Run("list by ids returns the owner's requested tracks", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		a, b, c := contractTrack(t, userId), contractTrack(t, userId), contractTrack(t, userId)
		for _, track := range []*domain.Track{a, b, c} {
			contractAdd(t, repo, track)
		}

		listed, err := repo.ListByIDs(ctx, userId, []domain.TrackId{a.ID, c.ID})
		if err != nil {
			t.Fatalf("ListByIDs: %v", err)
		}
		got := map[domain.TrackId]bool{}
		for _, track := range listed {
			got[track.ID] = true
		}
		if len(listed) != 2 || !got[a.ID] || !got[c.ID] {
			t.Errorf("ListByIDs returned %d tracks, want exactly a and c", len(listed))
		}
	})

	t.Run("list pages with limit and offset and reports the total", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		base := time.Now().UTC()
		ids := make([]domain.TrackId, 3)
		for i := range ids {
			track := contractTrack(t, userId)
			track.AddedAt = base.Add(time.Duration(i) * time.Second)
			contractAdd(t, repo, track)
			ids[i] = track.ID
		}

		page, total, err := repo.ListForUser(ctx, userId, 2, 0)
		if err != nil {
			t.Fatalf("ListForUser: %v", err)
		}
		if total != 3 || len(page) != 2 {
			t.Fatalf("first page = %d tracks of %d, want 2 of 3", len(page), total)
		}
		if page[0].ID != ids[2] || page[1].ID != ids[1] {
			t.Errorf("first page is not newest first")
		}

		rest, total, err := repo.ListForUser(ctx, userId, 10, 2)
		if err != nil {
			t.Fatalf("ListForUser offset: %v", err)
		}
		if total != 3 || len(rest) != 1 || rest[0].ID != ids[0] {
			t.Errorf("second page = %d tracks of %d, want the oldest track of 3", len(rest), total)
		}
	})

	t.Run("count stops at the bound and ignores other users", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		for range 3 {
			contractAdd(t, repo, contractTrack(t, userId))
		}
		contractAdd(t, repo, contractTrack(t, shared.NewUserId(uuid.New())))

		whole, err := repo.CountForUser(ctx, userId, 10)
		if err != nil {
			t.Fatalf("CountForUser(10): %v", err)
		}
		if whole != 3 {
			t.Errorf("CountForUser(10) = %d, want 3", whole)
		}
		bounded, err := repo.CountForUser(ctx, userId, 2)
		if err != nil {
			t.Fatalf("CountForUser(2): %v", err)
		}
		if bounded != 2 {
			t.Errorf("CountForUser(2) = %d, want 2", bounded)
		}
	})

	t.Run("update with the current version succeeds and bumps it", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		track := contractTrack(t, userId)
		contractAdd(t, repo, track)

		loaded := contractLoad(t, repo, track.ID, userId)
		before := loaded.Version
		edit := *loaded
		ref := "s3://bucket/update-" + uuid.NewString() + ".opus"
		if err := edit.MarkReady(ref); err != nil {
			t.Fatalf("MarkReady: %v", err)
		}
		if err := repo.Update(ctx, &edit, before); err != nil {
			t.Fatalf("Update: %v", err)
		}

		got := contractLoad(t, repo, track.ID, userId)
		if got.Version != before+1 {
			t.Errorf("Version = %d, want %d", got.Version, before+1)
		}
		if got.AcquisitionStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != ref {
			t.Errorf("update not persisted: status %v ref %v", got.AcquisitionStatus, got.AudioRef)
		}
	})

	t.Run("update with a stale version is a conflict", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		track := contractTrack(t, userId)
		contractAdd(t, repo, track)

		loaded := contractLoad(t, repo, track.ID, userId)
		before := loaded.Version
		stale := *loaded
		fresh := *loaded
		if err := fresh.MarkReady("s3://bucket/fresh-" + uuid.NewString() + ".opus"); err != nil {
			t.Fatalf("MarkReady: %v", err)
		}
		if err := repo.Update(ctx, &fresh, before); err != nil {
			t.Fatalf("Update: %v", err)
		}

		if err := stale.MarkReady("s3://bucket/stale-" + uuid.NewString() + ".opus"); err != nil {
			t.Fatalf("MarkReady: %v", err)
		}
		err := repo.Update(ctx, &stale, before)
		if !errors.Is(err, ports.ErrTrackVersionConflict) {
			t.Errorf("Update with stale version = %v, want ErrTrackVersionConflict", err)
		}
	})

	t.Run("update of a deleted row is not a conflict", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		track := contractTrack(t, userId)
		contractAdd(t, repo, track)

		loaded := *contractLoad(t, repo, track.ID, userId)
		if _, _, err := repo.Delete(ctx, track.ID, userId); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if err := loaded.MarkReady("s3://bucket/gone-" + uuid.NewString() + ".opus"); err != nil {
			t.Fatalf("MarkReady: %v", err)
		}
		err := repo.Update(ctx, &loaded, loaded.Version)
		if err == nil {
			t.Fatal("Update of a deleted row returned nil, want an error")
		}
		if errors.Is(err, ports.ErrTrackVersionConflict) {
			t.Errorf("Update of a deleted row = %v, want not-found rather than a version conflict", err)
		}
	})

	t.Run("delete returns the audio ref and removes the track", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		track := contractTrack(t, userId)
		ref := "s3://bucket/delete-" + uuid.NewString() + ".opus"
		if err := track.MarkReady(ref); err != nil {
			t.Fatalf("MarkReady: %v", err)
		}
		contractAdd(t, repo, track)

		deleted, got, err := repo.Delete(ctx, track.ID, userId)
		if err != nil {
			t.Fatalf("Delete: %v", err)
		}
		if !deleted || got == nil || *got != ref {
			t.Errorf("Delete = %v/%v, want true/%q", deleted, got, ref)
		}
		gone, err := repo.GetByID(ctx, track.ID, userId)
		if err != nil || gone != nil {
			t.Errorf("GetByID after delete = %v/%v, want nil/nil", gone, err)
		}
		again, _, err := repo.Delete(ctx, track.ID, userId)
		if err != nil || again {
			t.Errorf("second Delete = %v/%v, want false/nil", again, err)
		}
	})

	t.Run("audio ref in use sees another track and ignores the excluded one", func(t *testing.T) {
		repo := newRepo(t)
		userId := shared.NewUserId(uuid.New())
		holder := contractTrack(t, userId)
		ref := "s3://bucket/shared-" + uuid.NewString() + ".opus"
		if err := holder.MarkReady(ref); err != nil {
			t.Fatalf("MarkReady: %v", err)
		}
		contractAdd(t, repo, holder)
		other := contractTrack(t, userId)
		contractAdd(t, repo, other)

		inUse, err := repo.AudioRefInUse(ctx, ref, other.ID)
		if err != nil || !inUse {
			t.Errorf("AudioRefInUse excluding another track = %v/%v, want true/nil", inUse, err)
		}
		inUse, err = repo.AudioRefInUse(ctx, ref, holder.ID)
		if err != nil || inUse {
			t.Errorf("AudioRefInUse excluding the holder = %v/%v, want false/nil", inUse, err)
		}
	})
}
