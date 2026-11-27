package contracttest

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"
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

func contractTrack(t *testing.T, userId sharedids.UserId) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Title-"+randomIDs.NewString()[:8], "Artist-"+randomIDs.NewString()[:8], "Album", time.Now, randomIDs)
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

func contractLoad(t *testing.T, repo TrackRepositoryUnderTest, id domain.TrackId, userId sharedids.UserId) *domain.Track {
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

type trackRepositorySuite struct {
	ctx     context.Context
	newRepo func(t *testing.T) TrackRepositoryUnderTest
}

func TrackRepositoryContract(t *testing.T, newRepo func(t *testing.T) TrackRepositoryUnderTest) {
	suite := &trackRepositorySuite{ctx: context.Background(), newRepo: newRepo}

	t.Run("add then get returns the same fields", suite.addThenGetReturnsSameFields)
	t.Run("get of an unknown id is ErrTrackNotFound", suite.getUnknownIdIsNotFound)
	t.Run("a second add with the same dedup key returns the stored track", suite.secondAddWithSameDedupKeyReturnsStoredTrack)
	t.Run("another user's track is invisible", suite.anotherUsersTrackIsInvisible)
	t.Run("list by ids returns the owner's requested tracks", suite.listByIDsReturnsOwnersRequestedTracks)
	t.Run("list pages with limit and offset and reports the total", suite.listPagesWithLimitOffsetAndTotal)
	t.Run("count stops at the bound and ignores other users", suite.countStopsAtBoundAndIgnoresOtherUsers)
	t.Run("update with the current version succeeds and bumps it", suite.updateWithCurrentVersionBumpsIt)
	t.Run("update with a stale version is a conflict", suite.updateWithStaleVersionIsConflict)
	t.Run("update of a deleted row is not a conflict", suite.updateOfDeletedRowIsNotConflict)
	t.Run("delete returns the audio ref and removes the track", suite.deleteReturnsAudioRefAndRemovesTrack)
	t.Run("audio ref in use sees another track and ignores the excluded one", suite.audioRefInUseHonoursExclusion)
}

func (s *trackRepositorySuite) addThenGetReturnsSameFields(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	track := contractTrack(t, userId)
	contractAdd(t, repo, track)

	got := contractLoad(t, repo, track.ID, userId)
	if got.ID != track.ID || got.UserId != userId {
		t.Errorf("identity = %v/%v, want %v/%v", got.ID, got.UserId, track.ID, userId)
	}
	if got.Title != track.Title || got.Artist != track.Artist || got.Album != track.Album {
		t.Errorf("text = %q/%q/%q, want %q/%q/%q", got.Title, got.Artist, got.Album, track.Title, track.Artist, track.Album)
	}
	if got.DownloadStatus != domain.AcquisitionPending {
		t.Errorf("AcquisitionStatus = %v, want pending", got.DownloadStatus)
	}
	if got.DedupKey != track.DedupKey {
		t.Errorf("DedupKey = %q, want %q", got.DedupKey, track.DedupKey)
	}
}

func (s *trackRepositorySuite) getUnknownIdIsNotFound(t *testing.T) {
	repo := s.newRepo(t)
	got, err := repo.GetByID(s.ctx, randomIDs.NewTrackId(), randomIDs.NewUserId())
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Fatalf("GetByID error = %v, want ErrTrackNotFound", err)
	}
	if got != nil {
		t.Errorf("GetByID = %v, want nil", got.ID)
	}
}

func (s *trackRepositorySuite) secondAddWithSameDedupKeyReturnsStoredTrack(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	first := contractTrack(t, userId)
	contractAdd(t, repo, first)

	second, err := domain.NewTrack(userId, first.Title, first.Artist, first.Album, time.Now, randomIDs)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	stored, created, err := repo.Add(s.ctx, second)
	if err != nil {
		t.Fatalf("second Add: %v", err)
	}
	if created {
		t.Error("second Add created = true, want false")
	}
	if stored == nil || stored.ID != first.ID {
		t.Errorf("second Add returned %v, want the first track %v", stored, first.ID)
	}
}

func (s *trackRepositorySuite) anotherUsersTrackIsInvisible(t *testing.T) {
	repo := s.newRepo(t)
	owner := randomIDs.NewUserId()
	intruder := randomIDs.NewUserId()
	track := contractTrack(t, owner)
	contractAdd(t, repo, track)

	s.expectGetHiddenFromIntruder(t, repo, track.ID, intruder)
	s.expectListByIDsHiddenFromIntruder(t, repo, track.ID, intruder)
	s.expectDeleteRefusedForIntruder(t, repo, track.ID, intruder)
	contractLoad(t, repo, track.ID, owner)
}

func (s *trackRepositorySuite) expectGetHiddenFromIntruder(t *testing.T, repo TrackRepositoryUnderTest, id domain.TrackId, intruder sharedids.UserId) {
	got, err := repo.GetByID(s.ctx, id, intruder)
	if !errors.Is(err, domain.ErrTrackNotFound) {
		t.Fatalf("GetByID error = %v, want ErrTrackNotFound", err)
	}
	if got != nil {
		t.Error("GetByID exposed another user's track")
	}
}

func (s *trackRepositorySuite) expectListByIDsHiddenFromIntruder(t *testing.T, repo TrackRepositoryUnderTest, id domain.TrackId, intruder sharedids.UserId) {
	listed, err := repo.ListByIDs(s.ctx, intruder, []domain.TrackId{id})
	if err != nil {
		t.Fatalf("ListByIDs: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("ListByIDs returned %d tracks for another user, want 0", len(listed))
	}
}

func (s *trackRepositorySuite) expectDeleteRefusedForIntruder(t *testing.T, repo TrackRepositoryUnderTest, id domain.TrackId, intruder sharedids.UserId) {
	deleted, ref, err := repo.Delete(s.ctx, id, intruder)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if deleted || ref != nil {
		t.Errorf("Delete = %v/%v for another user, want false/nil", deleted, ref)
	}
}

func (s *trackRepositorySuite) listByIDsReturnsOwnersRequestedTracks(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	a, b, c := contractTrack(t, userId), contractTrack(t, userId), contractTrack(t, userId)
	for _, track := range []*domain.Track{a, b, c} {
		contractAdd(t, repo, track)
	}

	listed, err := repo.ListByIDs(s.ctx, userId, []domain.TrackId{a.ID, c.ID})
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
}

func (s *trackRepositorySuite) listPagesWithLimitOffsetAndTotal(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	base := time.Now().UTC()
	ids := make([]domain.TrackId, 3)
	for i := range ids {
		track := contractTrack(t, userId)
		track.AddedAt = base.Add(time.Duration(i) * time.Second)
		contractAdd(t, repo, track)
		ids[i] = track.ID
	}

	page, total, err := repo.ListForUser(s.ctx, userId, 2, 0)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if total != 3 || len(page) != 2 {
		t.Fatalf("first page = %d tracks of %d, want 2 of 3", len(page), total)
	}
	if page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Errorf("first page is not newest first")
	}

	rest, total, err := repo.ListForUser(s.ctx, userId, 10, 2)
	if err != nil {
		t.Fatalf("ListForUser offset: %v", err)
	}
	if total != 3 || len(rest) != 1 || rest[0].ID != ids[0] {
		t.Errorf("second page = %d tracks of %d, want the oldest track of 3", len(rest), total)
	}
}

func (s *trackRepositorySuite) countStopsAtBoundAndIgnoresOtherUsers(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	for range 3 {
		contractAdd(t, repo, contractTrack(t, userId))
	}
	contractAdd(t, repo, contractTrack(t, randomIDs.NewUserId()))

	whole, err := repo.CountForUser(s.ctx, userId, 10)
	if err != nil {
		t.Fatalf("CountForUser(10): %v", err)
	}
	if whole != 3 {
		t.Errorf("CountForUser(10) = %d, want 3", whole)
	}
	bounded, err := repo.CountForUser(s.ctx, userId, 2)
	if err != nil {
		t.Fatalf("CountForUser(2): %v", err)
	}
	if bounded != 2 {
		t.Errorf("CountForUser(2) = %d, want 2", bounded)
	}
}

func (s *trackRepositorySuite) updateWithCurrentVersionBumpsIt(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	track := contractTrack(t, userId)
	contractAdd(t, repo, track)

	loaded := contractLoad(t, repo, track.ID, userId)
	before := loaded.Version
	edit := *loaded
	ref := "s3://bucket/update-" + randomIDs.NewString() + ".opus"
	if err := edit.MarkReady(ref, randomIDs); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := repo.Update(s.ctx, &edit, before); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := contractLoad(t, repo, track.ID, userId)
	if got.Version != before+1 {
		t.Errorf("Version = %d, want %d", got.Version, before+1)
	}
	if got.DownloadStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != ref {
		t.Errorf("update not persisted: status %v ref %v", got.DownloadStatus, got.AudioRef)
	}
}

func (s *trackRepositorySuite) updateWithStaleVersionIsConflict(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	track := contractTrack(t, userId)
	contractAdd(t, repo, track)

	loaded := contractLoad(t, repo, track.ID, userId)
	before := loaded.Version
	stale := *loaded
	fresh := *loaded
	if err := fresh.MarkReady("s3://bucket/fresh-"+randomIDs.NewString()+".opus", randomIDs); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := repo.Update(s.ctx, &fresh, before); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := stale.MarkReady("s3://bucket/stale-"+randomIDs.NewString()+".opus", randomIDs); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	err := repo.Update(s.ctx, &stale, before)
	if !errors.Is(err, ports.ErrTrackVersionConflict) {
		t.Errorf("Update with stale version = %v, want ErrTrackVersionConflict", err)
	}
}

func (s *trackRepositorySuite) updateOfDeletedRowIsNotConflict(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	track := contractTrack(t, userId)
	contractAdd(t, repo, track)

	loaded := *contractLoad(t, repo, track.ID, userId)
	if _, _, err := repo.Delete(s.ctx, track.ID, userId); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := loaded.MarkReady("s3://bucket/gone-"+randomIDs.NewString()+".opus", randomIDs); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	err := repo.Update(s.ctx, &loaded, loaded.Version)
	if err == nil {
		t.Fatal("Update of a deleted row returned nil, want an error")
	}
	if errors.Is(err, ports.ErrTrackVersionConflict) {
		t.Errorf("Update of a deleted row = %v, want not-found rather than a version conflict", err)
	}
}

func (s *trackRepositorySuite) deleteReturnsAudioRefAndRemovesTrack(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	track := contractTrack(t, userId)
	ref := "s3://bucket/delete-" + randomIDs.NewString() + ".opus"
	if err := track.MarkReady(ref, randomIDs); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	contractAdd(t, repo, track)

	deleted, got, err := repo.Delete(s.ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if !deleted || got == nil || *got != ref {
		t.Errorf("Delete = %v/%v, want true/%q", deleted, got, ref)
	}
	if _, err := repo.GetByID(s.ctx, track.ID, userId); !errors.Is(err, domain.ErrTrackNotFound) {
		t.Errorf("GetByID after delete: err = %v, want ErrTrackNotFound", err)
	}
	again, _, err := repo.Delete(s.ctx, track.ID, userId)
	if err != nil || again {
		t.Errorf("second Delete = %v/%v, want false/nil", again, err)
	}
}

func (s *trackRepositorySuite) audioRefInUseHonoursExclusion(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	holder := contractTrack(t, userId)
	ref := "s3://bucket/shared-" + randomIDs.NewString() + ".opus"
	if err := holder.MarkReady(ref, randomIDs); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	contractAdd(t, repo, holder)
	other := contractTrack(t, userId)
	contractAdd(t, repo, other)

	inUse, err := repo.AudioRefInUse(s.ctx, ref, other.ID)
	if err != nil || !inUse {
		t.Errorf("AudioRefInUse excluding another track = %v/%v, want true/nil", inUse, err)
	}
	inUse, err = repo.AudioRefInUse(s.ctx, ref, holder.ID)
	if err != nil || inUse {
		t.Errorf("AudioRefInUse excluding the holder = %v/%v, want false/nil", inUse, err)
	}
}

const contractLibraryCap = 3

type trackRepositoryLibraryCapSuite struct {
	ctx     context.Context
	newRepo func(t *testing.T, libraryCap int) TrackRepositoryUnderTest
}

func TrackRepositoryLibraryCapContract(t *testing.T, newRepo func(t *testing.T, libraryCap int) TrackRepositoryUnderTest) {
	suite := &trackRepositoryLibraryCapSuite{ctx: context.Background(), newRepo: newRepo}

	t.Run("an add past the cap is ErrLibraryFull and stores nothing", suite.addPastCapIsLibraryFullAndStoresNothing)
	t.Run("the cap is per user", suite.capIsPerUser)
	t.Run("a replay by idempotency key at the cap returns the stored track instead of refusing", suite.keyedReplayAtCapReturnsStoredTrack)
	t.Run("a replay of a stored track at the cap returns it instead of refusing", suite.dedupReplayAtCapReturnsStoredTrack)
}

func (s *trackRepositoryLibraryCapSuite) addPastCapIsLibraryFullAndStoresNothing(t *testing.T) {
	repo := s.newRepo(t, contractLibraryCap)
	userId := randomIDs.NewUserId()
	for range contractLibraryCap {
		contractAdd(t, repo, contractTrack(t, userId))
	}

	stored, created, err := repo.Add(s.ctx, contractTrack(t, userId))

	if !errors.Is(err, domain.ErrLibraryFull) || created || stored != nil {
		t.Fatalf("Add past the cap = %v/%v/%v, want nil/false/ErrLibraryFull", stored, created, err)
	}
	held, err := repo.CountForUser(s.ctx, userId, contractLibraryCap+1)
	if err != nil || held != contractLibraryCap {
		t.Errorf("CountForUser = %d/%v, want %d/nil", held, err, contractLibraryCap)
	}
}

func (s *trackRepositoryLibraryCapSuite) capIsPerUser(t *testing.T) {
	repo := s.newRepo(t, contractLibraryCap)
	full := randomIDs.NewUserId()
	for range contractLibraryCap {
		contractAdd(t, repo, contractTrack(t, full))
	}

	contractAdd(t, repo, contractTrack(t, randomIDs.NewUserId()))
}

func (s *trackRepositoryLibraryCapSuite) keyedReplayAtCapReturnsStoredTrack(t *testing.T) {
	repo := s.newRepo(t, contractLibraryCap)
	userId := randomIDs.NewUserId()
	key := "idem-" + randomIDs.NewString()
	first := contractTrack(t, userId)
	first.IdempotencyKey = &key
	contractAdd(t, repo, first)
	for range contractLibraryCap - 1 {
		contractAdd(t, repo, contractTrack(t, userId))
	}
	replay := contractTrack(t, userId)
	replay.IdempotencyKey = &key

	stored, created, err := repo.Add(s.ctx, replay)

	if err != nil || created || stored == nil || stored.ID != first.ID {
		t.Fatalf("keyed replay at the cap = %v/%v/%v, want the first track, not created, no error", stored, created, err)
	}
}

func (s *trackRepositoryLibraryCapSuite) dedupReplayAtCapReturnsStoredTrack(t *testing.T) {
	repo := s.newRepo(t, contractLibraryCap)
	userId := randomIDs.NewUserId()
	first := contractTrack(t, userId)
	contractAdd(t, repo, first)
	for range contractLibraryCap - 1 {
		contractAdd(t, repo, contractTrack(t, userId))
	}
	replay, err := domain.NewTrack(userId, first.Title, first.Artist, first.Album, time.Now, randomIDs)
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}

	stored, created, err := repo.Add(s.ctx, replay)

	if err != nil || created || stored == nil || stored.ID != first.ID {
		t.Fatalf("replay at the cap = %v/%v/%v, want the first track, not created, no error", stored, created, err)
	}
}
