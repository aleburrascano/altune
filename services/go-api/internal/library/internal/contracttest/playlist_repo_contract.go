package contracttest

import (
	"altune/go-api/internal/library/internal/domain"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"
)

type PlaylistRepositoryUnderTest interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	GetByID(ctx context.Context, id domain.PlaylistId, userId sharedids.UserId) (*domain.Playlist, domain.PlaylistSummary, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId sharedids.UserId) (*domain.Playlist, []*domain.Track, error)
	Exists(ctx context.Context, playlistId domain.PlaylistId, userId sharedids.UserId) (bool, error)
	ListForUser(ctx context.Context, userId sharedids.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error)
	CountForUser(ctx context.Context, userId sharedids.UserId, atMost int) (int, error)
	Update(ctx context.Context, playlist *domain.Playlist) error
	Delete(ctx context.Context, id domain.PlaylistId, userId sharedids.UserId) (bool, error)
	AddTracks(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error)
	RemoveTracks(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) ([]domain.TrackId, error)
	ReorderTracks(ctx context.Context, userId sharedids.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
	SeedTrack(t *testing.T, userId sharedids.UserId) domain.TrackId
}

func contractPlaylist(t *testing.T, repo PlaylistRepositoryUnderTest, userId sharedids.UserId, createdAt time.Time) *domain.Playlist {
	t.Helper()
	pl, err := domain.NewPlaylist(userId, "Playlist-"+randomIDs.NewString()[:8], createdAt, randomIDs)
	if err != nil {
		t.Fatalf("NewPlaylist: %v", err)
	}
	if err := repo.Create(context.Background(), pl); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return pl
}

func contractSeedTracks(t *testing.T, repo PlaylistRepositoryUnderTest, userId sharedids.UserId, n int) []domain.TrackId {
	t.Helper()
	ids := make([]domain.TrackId, n)
	for i := range ids {
		ids[i] = repo.SeedTrack(t, userId)
	}
	return ids
}

func contractMembers(t *testing.T, repo PlaylistRepositoryUnderTest, pl *domain.Playlist) []domain.TrackId {
	t.Helper()
	got, tracks, err := repo.GetWithTracks(context.Background(), pl.ID, pl.UserId)
	if err != nil {
		t.Fatalf("GetWithTracks: %v", err)
	}
	if got == nil {
		t.Fatal("GetWithTracks returned nil playlist")
	}
	if len(got.Tracks) != len(tracks) {
		t.Fatalf("len(playlist.Tracks) = %d, len(tracks) = %d", len(got.Tracks), len(tracks))
	}
	ids := make([]domain.TrackId, len(tracks))
	for i, tr := range tracks {
		ids[i] = tr.ID()
		if got.Tracks[i].TrackId != tr.ID() || got.Tracks[i].Position != i {
			t.Errorf("playlist.Tracks[%d] = %v@%d, want %v@%d", i, got.Tracks[i].TrackId, got.Tracks[i].Position, tr.ID(), i)
		}
	}
	return ids
}

func contractSameIds(t *testing.T, label string, got, want []domain.TrackId) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

func PlaylistRepositoryContract(t *testing.T, newRepo func(t *testing.T) PlaylistRepositoryUnderTest) {
	suite := &playlistContractSuite{
		now:     time.Now().UTC(),
		newRepo: newRepo,
	}

	t.Run("create then get returns the playlist", suite.createThenGetReturnsPlaylist)
	t.Run("an unknown playlist is absent", suite.unknownPlaylistIsAbsent)
	t.Run("another user's playlist is invisible and untouchable", suite.foreignPlaylistIsInvisibleAndUntouchable)
	t.Run("update renames the playlist", suite.updateRenamesPlaylist)
	t.Run("update refuses a playlist that does not exist", suite.updateRefusesMissingPlaylist)
	t.Run("add tracks keeps request order and appends after existing members", suite.addTracksKeepsRequestOrderAndAppends)
	t.Run("add tracks skips members and repeats within the batch", suite.addTracksSkipsMembersAndRepeats)
	t.Run("remove tracks closes the gap and reports what it removed", suite.removeTracksClosesGapAndReportsRemoved)
	t.Run("reorder with a permutation succeeds", suite.reorderWithPermutationSucceeds)
	t.Run("reorder refuses a plan that is not exactly the members", suite.reorderRefusesPlanThatIsNotTheMembers)
	t.Run("list pages newest first and counts", suite.listPagesNewestFirstAndCounts)
	t.Run("delete removes the playlist once", suite.deleteRemovesPlaylistOnce)
}

type playlistContractSuite struct {
	now     time.Time
	newRepo func(t *testing.T) PlaylistRepositoryUnderTest
}

func (s *playlistContractSuite) createThenGetReturnsPlaylist(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)

	got, summary, err := repo.GetByID(t.Context(), pl.ID, userId)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil, want playlist")
	}
	if got.ID != pl.ID || got.UserId != userId || got.Name != pl.Name {
		t.Errorf("got %v/%v/%q, want %v/%v/%q", got.ID, got.UserId, got.Name, pl.ID, userId, pl.Name)
	}
	if summary.TrackCount != 0 {
		t.Errorf("TrackCount = %d, want 0", summary.TrackCount)
	}
	exists, err := repo.Exists(t.Context(), pl.ID, userId)
	if err != nil || !exists {
		t.Errorf("Exists = %v, %v, want true, nil", exists, err)
	}
}

func (s *playlistContractSuite) unknownPlaylistIsAbsent(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	missing := domain.PlaylistId{EntityId: randomIDs.NewEntityId()}

	got, _, err := repo.GetByID(t.Context(), missing, userId)
	if err != nil || got != nil {
		t.Errorf("GetByID = %v, %v, want nil, nil", got, err)
	}
	exists, err := repo.Exists(t.Context(), missing, userId)
	if err != nil || exists {
		t.Errorf("Exists = %v, %v, want false, nil", exists, err)
	}
}

type foreignPlaylistFixture struct {
	repo     PlaylistRepositoryUnderTest
	owner    sharedids.UserId
	intruder sharedids.UserId
	pl       *domain.Playlist
	tracks   []domain.TrackId
	mine     []domain.TrackId
}

func (s *playlistContractSuite) foreignPlaylistIsInvisibleAndUntouchable(t *testing.T) {
	repo := s.newRepo(t)
	owner := randomIDs.NewUserId()
	intruder := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, owner, s.now)
	tracks := contractSeedTracks(t, repo, owner, 2)
	mine := contractSeedTracks(t, repo, intruder, 1)
	if _, err := repo.AddTracks(t.Context(), owner, pl.ID, tracks); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}
	fx := foreignPlaylistFixture{repo: repo, owner: owner, intruder: intruder, pl: pl, tracks: tracks, mine: mine}

	s.intruderCannotSeeOrDelete(t, fx)
	s.intruderCannotModify(t, fx)
	s.ownerPlaylistIsIntact(t, fx)
}

func (s *playlistContractSuite) intruderCannotSeeOrDelete(t *testing.T, fx foreignPlaylistFixture) {
	got, _, err := fx.repo.GetByID(t.Context(), fx.pl.ID, fx.intruder)
	if err != nil || got != nil {
		t.Errorf("GetByID = %v, %v, want nil, nil", got, err)
	}
	withTracks, list, err := fx.repo.GetWithTracks(t.Context(), fx.pl.ID, fx.intruder)
	if err != nil || withTracks != nil || len(list) != 0 {
		t.Errorf("GetWithTracks = %v, %v, %v, want nil, empty, nil", withTracks, list, err)
	}
	exists, err := fx.repo.Exists(t.Context(), fx.pl.ID, fx.intruder)
	if err != nil || exists {
		t.Errorf("Exists = %v, %v, want false, nil", exists, err)
	}
	deleted, err := fx.repo.Delete(t.Context(), fx.pl.ID, fx.intruder)
	if err != nil || deleted {
		t.Errorf("Delete = %v, %v, want false, nil", deleted, err)
	}
}

func (s *playlistContractSuite) intruderCannotModify(t *testing.T, fx foreignPlaylistFixture) {
	hijack := *fx.pl
	hijack.UserId = fx.intruder
	hijack.Name = "hijacked"
	if err := fx.repo.Update(t.Context(), &hijack); !errors.Is(err, domain.ErrPlaylistNotOwned) {
		t.Errorf("Update error = %v, want ErrPlaylistNotOwned", err)
	}
	if _, err := fx.repo.AddTracks(t.Context(), fx.intruder, fx.pl.ID, fx.mine); !errors.Is(err, domain.ErrPlaylistNotOwned) {
		t.Errorf("AddTracks error = %v, want ErrPlaylistNotOwned", err)
	}
	if _, err := fx.repo.RemoveTracks(t.Context(), fx.intruder, fx.pl.ID, fx.tracks[:1]); !errors.Is(err, domain.ErrPlaylistNotOwned) {
		t.Errorf("RemoveTracks error = %v, want ErrPlaylistNotOwned", err)
	}
	plan := []domain.PlaylistTrack{{TrackId: fx.tracks[1], Position: 0}, {TrackId: fx.tracks[0], Position: 1}}
	if err := fx.repo.ReorderTracks(t.Context(), fx.intruder, fx.pl.ID, plan); !errors.Is(err, domain.ErrPlaylistNotOwned) {
		t.Errorf("ReorderTracks error = %v, want ErrPlaylistNotOwned", err)
	}
}

func (s *playlistContractSuite) ownerPlaylistIsIntact(t *testing.T, fx foreignPlaylistFixture) {
	owned, _, err := fx.repo.GetByID(t.Context(), fx.pl.ID, fx.owner)
	if err != nil || owned == nil || owned.Name != fx.pl.Name {
		t.Errorf("owner's playlist after attacks = %v, %v, want name %q", owned, err, fx.pl.Name)
	}
	contractSameIds(t, "owner's members", contractMembers(t, fx.repo, fx.pl), fx.tracks)
}

func (s *playlistContractSuite) updateRenamesPlaylist(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)

	renamed := *pl
	renamed.Name = "Renamed-" + randomIDs.NewString()[:8]
	renamed.UpdatedAt = s.now.Add(time.Minute)
	if err := repo.Update(t.Context(), &renamed); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _, err := repo.GetByID(t.Context(), pl.ID, userId)
	if err != nil || got == nil {
		t.Fatalf("GetByID = %v, %v", got, err)
	}
	if got.Name != renamed.Name {
		t.Errorf("Name = %q, want %q", got.Name, renamed.Name)
	}
}

func (s *playlistContractSuite) updateRefusesMissingPlaylist(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	ghost, err := domain.NewPlaylist(userId, "Ghost", s.now, randomIDs)
	if err != nil {
		t.Fatalf("NewPlaylist: %v", err)
	}
	if err := repo.Update(t.Context(), ghost); !errors.Is(err, domain.ErrPlaylistNotOwned) {
		t.Errorf("Update error = %v, want ErrPlaylistNotOwned", err)
	}
}

func (s *playlistContractSuite) addTracksKeepsRequestOrderAndAppends(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)
	ids := contractSeedTracks(t, repo, userId, 4)

	added, err := repo.AddTracks(t.Context(), userId, pl.ID, []domain.TrackId{ids[2], ids[0]})
	if err != nil {
		t.Fatalf("AddTracks: %v", err)
	}
	contractSameIds(t, "added", added, []domain.TrackId{ids[2], ids[0]})

	added, err = repo.AddTracks(t.Context(), userId, pl.ID, []domain.TrackId{ids[3], ids[1]})
	if err != nil {
		t.Fatalf("AddTracks second batch: %v", err)
	}
	contractSameIds(t, "added second batch", added, []domain.TrackId{ids[3], ids[1]})

	contractSameIds(t, "members", contractMembers(t, repo, pl), []domain.TrackId{ids[2], ids[0], ids[3], ids[1]})
	_, summary, err := repo.GetByID(t.Context(), pl.ID, userId)
	if err != nil || summary.TrackCount != 4 {
		t.Errorf("TrackCount = %d, %v, want 4", summary.TrackCount, err)
	}
}

func (s *playlistContractSuite) addTracksSkipsMembersAndRepeats(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)
	ids := contractSeedTracks(t, repo, userId, 3)
	if _, err := repo.AddTracks(t.Context(), userId, pl.ID, ids[:1]); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}

	added, err := repo.AddTracks(t.Context(), userId, pl.ID, []domain.TrackId{ids[0], ids[1], ids[1], ids[2]})
	if err != nil {
		t.Fatalf("AddTracks with duplicates: %v", err)
	}
	contractSameIds(t, "added", added, ids[1:])
	contractSameIds(t, "members", contractMembers(t, repo, pl), ids)

	added, err = repo.AddTracks(t.Context(), userId, pl.ID, ids)
	if err != nil {
		t.Fatalf("AddTracks of only members: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("added = %v, want none", added)
	}
	contractSameIds(t, "members after no-op", contractMembers(t, repo, pl), ids)
}

func (s *playlistContractSuite) removeTracksClosesGapAndReportsRemoved(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)
	ids := contractSeedTracks(t, repo, userId, 5)
	stranger := contractSeedTracks(t, repo, userId, 1)[0]
	if _, err := repo.AddTracks(t.Context(), userId, pl.ID, ids); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}

	removed, err := repo.RemoveTracks(t.Context(), userId, pl.ID, []domain.TrackId{ids[3], stranger, ids[1], ids[3]})
	if err != nil {
		t.Fatalf("RemoveTracks: %v", err)
	}
	contractSameIds(t, "removed", removed, []domain.TrackId{ids[3], ids[1]})
	contractSameIds(t, "members", contractMembers(t, repo, pl), []domain.TrackId{ids[0], ids[2], ids[4]})
	_, summary, err := repo.GetByID(t.Context(), pl.ID, userId)
	if err != nil || summary.TrackCount != 3 {
		t.Errorf("TrackCount = %d, %v, want 3", summary.TrackCount, err)
	}

	removed, err = repo.RemoveTracks(t.Context(), userId, pl.ID, []domain.TrackId{ids[1]})
	if err != nil || len(removed) != 0 {
		t.Errorf("RemoveTracks of a non-member = %v, %v, want none, nil", removed, err)
	}
}

func (s *playlistContractSuite) reorderWithPermutationSucceeds(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)
	ids := contractSeedTracks(t, repo, userId, 3)
	if _, err := repo.AddTracks(t.Context(), userId, pl.ID, ids); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}

	plan := []domain.PlaylistTrack{
		{TrackId: ids[2], Position: 0},
		{TrackId: ids[0], Position: 1},
		{TrackId: ids[1], Position: 2},
	}
	if err := repo.ReorderTracks(t.Context(), userId, pl.ID, plan); err != nil {
		t.Fatalf("ReorderTracks: %v", err)
	}
	contractSameIds(t, "members", contractMembers(t, repo, pl), []domain.TrackId{ids[2], ids[0], ids[1]})
}

func (s *playlistContractSuite) reorderRefusesPlanThatIsNotTheMembers(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)
	ids := contractSeedTracks(t, repo, userId, 3)
	outsider := contractSeedTracks(t, repo, userId, 1)[0]
	if _, err := repo.AddTracks(t.Context(), userId, pl.ID, ids[:2]); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}

	plans := map[string][]domain.PlaylistTrack{
		"missing a member":   {{TrackId: ids[1], Position: 0}},
		"an extra track":     {{TrackId: ids[1], Position: 0}, {TrackId: ids[0], Position: 1}, {TrackId: ids[2], Position: 2}},
		"a swapped-in track": {{TrackId: ids[1], Position: 0}, {TrackId: outsider, Position: 1}},
	}
	for name, plan := range plans {
		err := repo.ReorderTracks(t.Context(), userId, pl.ID, plan)
		if !errors.Is(err, domain.ErrPlaylistChangedDuringReorder) {
			t.Errorf("%s: error = %v, want ErrPlaylistChangedDuringReorder", name, err)
		}
	}
	contractSameIds(t, "members", contractMembers(t, repo, pl), ids[:2])
}

func (s *playlistContractSuite) listPagesNewestFirstAndCounts(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	other := randomIDs.NewUserId()
	var created []*domain.Playlist
	for i := range 3 {
		created = append(created, contractPlaylist(t, repo, userId, s.now.Add(time.Duration(i)*time.Minute)))
	}
	contractPlaylist(t, repo, other, s.now)
	ids := contractSeedTracks(t, repo, userId, 2)
	if _, err := repo.AddTracks(t.Context(), userId, created[0].ID, ids); err != nil {
		t.Fatalf("AddTracks: %v", err)
	}

	first, err := repo.ListForUser(t.Context(), userId, 2, 0)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	second, err := repo.ListForUser(t.Context(), userId, 2, 2)
	if err != nil {
		t.Fatalf("ListForUser second page: %v", err)
	}
	if len(first) != 2 || len(second) != 1 {
		t.Fatalf("page sizes = %d, %d, want 2, 1", len(first), len(second))
	}
	s.assertPagesNewestFirst(t, first, second, created)
	s.assertPageTrackCounts(t, append(first, second...), created[0].ID)

	beyond, err := repo.ListForUser(t.Context(), userId, 2, 3)
	if err != nil || len(beyond) != 0 {
		t.Errorf("page past the end = %d rows, %v, want none", len(beyond), err)
	}
	s.assertCountForUser(t, repo, userId)
}

func (s *playlistContractSuite) assertPagesNewestFirst(t *testing.T, first, second []domain.PlaylistWithSummary, created []*domain.Playlist) {
	want := []domain.PlaylistId{created[2].ID, created[1].ID, created[0].ID}
	got := []domain.PlaylistId{first[0].Playlist.ID, first[1].Playlist.ID, second[0].Playlist.ID}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("order[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func (s *playlistContractSuite) assertPageTrackCounts(t *testing.T, rows []domain.PlaylistWithSummary, withTwoTracks domain.PlaylistId) {
	for _, row := range rows {
		wantCount := 0
		if row.Playlist.ID == withTwoTracks {
			wantCount = 2
		}
		if row.Summary.TrackCount != wantCount {
			t.Errorf("TrackCount of %v = %d, want %d", row.Playlist.ID, row.Summary.TrackCount, wantCount)
		}
	}
}

func (s *playlistContractSuite) assertCountForUser(t *testing.T, repo PlaylistRepositoryUnderTest, userId sharedids.UserId) {
	held, err := repo.CountForUser(t.Context(), userId, 10)
	if err != nil || held != 3 {
		t.Errorf("CountForUser = %d, %v, want 3", held, err)
	}
	held, err = repo.CountForUser(t.Context(), userId, 2)
	if err != nil || held != 2 {
		t.Errorf("CountForUser capped at 2 = %d, %v, want 2", held, err)
	}
}

func (s *playlistContractSuite) deleteRemovesPlaylistOnce(t *testing.T) {
	repo := s.newRepo(t)
	userId := randomIDs.NewUserId()
	pl := contractPlaylist(t, repo, userId, s.now)

	deleted, err := repo.Delete(t.Context(), pl.ID, userId)
	if err != nil || !deleted {
		t.Fatalf("Delete = %v, %v, want true, nil", deleted, err)
	}
	got, _, err := repo.GetByID(t.Context(), pl.ID, userId)
	if err != nil || got != nil {
		t.Errorf("GetByID after delete = %v, %v, want nil, nil", got, err)
	}
	deleted, err = repo.Delete(t.Context(), pl.ID, userId)
	if err != nil || deleted {
		t.Errorf("second Delete = %v, %v, want false, nil", deleted, err)
	}
	held, err := repo.CountForUser(t.Context(), userId, 10)
	if err != nil || held != 0 {
		t.Errorf("CountForUser after delete = %d, %v, want 0", held, err)
	}
}
