package contracttest

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/ids"
	"testing"
	"time"
)

type orphanedAudioSuite struct {
	newQueue func(t *testing.T) (ports.OrphanedAudioQueue, func(track *domain.Track))
}

func OrphanedAudioQueueContract(t *testing.T, newQueue func(t *testing.T) (ports.OrphanedAudioQueue, func(track *domain.Track))) {
	suite := &orphanedAudioSuite{newQueue: newQueue}

	t.Run("a recorded ref is listed with its owner, track and a timestamp", suite.recordedRefIsListedWithDetails)
	t.Run("a ref recorded with a future not-before is listed only once it passes", suite.futureNotBeforeIsListedOnlyOncePassed)
	t.Run("recording the same ref twice lists it once", suite.recordingSameRefTwiceListsItOnce)
	t.Run("list honours the limit", suite.listHonoursLimit)
	t.Run("resolve removes the ref", suite.resolveRemovesRef)
	t.Run("marking an attempt raises Attempts", suite.markingAttemptRaisesAttempts)
	t.Run("recording again resets Attempts", suite.recordingAgainResetsAttempts)
	t.Run("marking an unknown ref is not an error and lists nothing", suite.markingUnknownRefListsNothing)
	t.Run("list puts attempted refs after never-attempted ones", suite.listPutsAttemptedRefsLast)
	t.Run("audio usage", suite.audioUsageReflectsReferencesAndAcquisitions)
}

func (c *orphanedAudioSuite) refFor(userId ids.UserId) string {
	return userId.String() + "/artist/album/title-" + randomIDs.NewString()[:8] + ".mp3"
}

func (c *orphanedAudioSuite) record(t *testing.T, q ports.OrphanedAudioQueue, userId ids.UserId, ref string) ports.OrphanedAudio {
	t.Helper()
	orphan := ports.OrphanedAudio{AudioRef: ref, UserId: userId, TrackId: randomIDs.NewTrackId()}
	if err := q.RecordOrphanedAudio(t.Context(), orphan); err != nil {
		t.Fatalf("RecordOrphanedAudio: %v", err)
	}
	return orphan
}

func (c *orphanedAudioSuite) listed(t *testing.T, q ports.OrphanedAudioQueue, ref string) (ports.OrphanedAudio, int) {
	t.Helper()
	all, err := q.ListOrphanedAudio(t.Context(), 10_000)
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

func (c *orphanedAudioSuite) usage(t *testing.T, q ports.OrphanedAudioQueue, ref string, owner ids.UserId) ports.AudioUsage {
	t.Helper()
	got, err := q.AudioUsage(t.Context(), ref, owner)
	if err != nil {
		t.Fatalf("AudioUsage: %v", err)
	}
	return got
}

func (c *orphanedAudioSuite) newTrack(t *testing.T, userId ids.UserId, audioRef string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(domain.NewTrackInput{UserId: userId, Title: "Title-" + randomIDs.NewString()[:8], Artist: "Artist-" + randomIDs.NewString()[:8], Album: "Album"}, time.Now, randomIDs)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if audioRef == "" {
		return track
	}
	if err := track.MarkReady(audioRef, randomIDs); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	return track
}

func (c *orphanedAudioSuite) recordedRefIsListedWithDetails(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	orphan := c.record(t, q, userId, c.refFor(userId))
	got, count := c.listed(t, q, orphan.AudioRef)
	if count != 1 || got.UserId != userId || got.TrackId != orphan.TrackId || got.Attempts != 0 || got.RecordedAt.IsZero() {
		t.Fatalf("listed = %+v (x%d), want owner, track, zero attempts, a timestamp", got, count)
	}
}

func (c *orphanedAudioSuite) futureNotBeforeIsListedOnlyOncePassed(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	held, due := c.refFor(userId), c.refFor(userId)
	for ref, notBefore := range map[string]time.Time{held: time.Now().Add(time.Hour), due: time.Now().Add(-time.Minute)} {
		orphan := ports.OrphanedAudio{AudioRef: ref, UserId: userId, TrackId: randomIDs.NewTrackId(), NotBefore: notBefore}
		if err := q.RecordOrphanedAudio(t.Context(), orphan); err != nil {
			t.Fatalf("RecordOrphanedAudio: %v", err)
		}
	}
	if _, count := c.listed(t, q, held); count != 0 {
		t.Errorf("held ref listed %d times before its not-before, want 0", count)
	}
	if _, count := c.listed(t, q, due); count != 1 {
		t.Errorf("due ref listed %d times, want 1", count)
	}
}

func (c *orphanedAudioSuite) recordingSameRefTwiceListsItOnce(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	ref := c.refFor(userId)
	c.record(t, q, userId, ref)
	second := c.record(t, q, userId, ref)
	got, count := c.listed(t, q, ref)
	if count != 1 {
		t.Fatalf("ref listed %d times, want 1", count)
	}
	if got.TrackId != second.TrackId {
		t.Errorf("track = %v, want the latest recorded %v", got.TrackId, second.TrackId)
	}
}

func (c *orphanedAudioSuite) listHonoursLimit(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	for range 3 {
		c.record(t, q, userId, c.refFor(userId))
	}
	got, err := q.ListOrphanedAudio(t.Context(), 2)
	if err != nil {
		t.Fatalf("ListOrphanedAudio: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("listed %d with limit 2, want 2", len(got))
	}
}

func (c *orphanedAudioSuite) resolveRemovesRef(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	ref := c.refFor(userId)
	c.record(t, q, userId, ref)
	if err := q.ResolveOrphanedAudio(t.Context(), ref); err != nil {
		t.Fatalf("ResolveOrphanedAudio: %v", err)
	}
	if _, count := c.listed(t, q, ref); count != 0 {
		t.Error("resolved ref still listed")
	}
}

func (c *orphanedAudioSuite) markingAttemptRaisesAttempts(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	ref := c.refFor(userId)
	c.record(t, q, userId, ref)
	for want := 1; want <= 2; want++ {
		if err := q.MarkOrphanedAudioAttempt(t.Context(), ref, "storage down"); err != nil {
			t.Fatalf("MarkOrphanedAudioAttempt: %v", err)
		}
		if got, _ := c.listed(t, q, ref); got.Attempts != want {
			t.Errorf("attempts = %d, want %d", got.Attempts, want)
		}
	}
}

func (c *orphanedAudioSuite) recordingAgainResetsAttempts(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	ref := c.refFor(userId)
	c.record(t, q, userId, ref)
	if err := q.MarkOrphanedAudioAttempt(t.Context(), ref, "storage down"); err != nil {
		t.Fatal(err)
	}
	c.record(t, q, userId, ref)
	if got, _ := c.listed(t, q, ref); got.Attempts != 0 {
		t.Errorf("attempts after re-record = %d, want 0", got.Attempts)
	}
}

func (c *orphanedAudioSuite) markingUnknownRefListsNothing(t *testing.T) {
	q, _ := c.newQueue(t)
	ref := c.refFor(randomIDs.NewUserId())
	if err := q.MarkOrphanedAudioAttempt(t.Context(), ref, "x"); err != nil {
		t.Fatalf("MarkOrphanedAudioAttempt: %v", err)
	}
	if _, count := c.listed(t, q, ref); count != 0 {
		t.Error("marking an unknown ref created an entry")
	}
}

func (c *orphanedAudioSuite) listPutsAttemptedRefsLast(t *testing.T) {
	q, _ := c.newQueue(t)
	userId := randomIDs.NewUserId()
	stuck, fresh := c.refFor(userId), c.refFor(userId)
	c.record(t, q, userId, stuck)
	c.record(t, q, userId, fresh)
	if err := q.MarkOrphanedAudioAttempt(t.Context(), stuck, "storage down"); err != nil {
		t.Fatal(err)
	}
	all, err := q.ListOrphanedAudio(t.Context(), 10_000)
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
}

func (c *orphanedAudioSuite) audioUsageReflectsReferencesAndAcquisitions(t *testing.T) {
	q, seed := c.newQueue(t)

	owner := randomIDs.NewUserId()
	ref := c.refFor(owner)
	if got := c.usage(t, q, ref, owner); got != ports.AudioUnused {
		t.Errorf("unreferenced ref usage = %v, want unused", got)
	}

	other := randomIDs.NewUserId()
	seed(c.newTrack(t, other, ref))
	if got := c.usage(t, q, ref, owner); got != ports.AudioReferenced {
		t.Errorf("ref held by a ready track usage = %v, want referenced", got)
	}

	busyOwner := randomIDs.NewUserId()
	busyRef := c.refFor(busyOwner)
	seed(c.newTrack(t, busyOwner, ""))
	if got := c.usage(t, q, busyRef, busyOwner); got != ports.AudioOwnerAcquiring {
		t.Errorf("owner with a track still acquiring usage = %v, want owner acquiring", got)
	}
	if got := c.usage(t, q, busyRef, owner); got != ports.AudioUnused {
		t.Errorf("another user's acquiring track blocked the ref: usage = %v, want unused", got)
	}
}
