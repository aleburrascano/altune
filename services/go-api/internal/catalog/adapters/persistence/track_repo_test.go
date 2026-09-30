package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func newTestTrackForDB(t *testing.T, userId shared.UserId) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Title-"+uuid.New().String()[:8], "Artist-"+uuid.New().String()[:8], "Album")
	if err != nil {
		t.Fatalf("newTestTrackForDB: %v", err)
	}
	return track
}

func cleanupTrack(t *testing.T, pool *pgxpool.Pool, id domain.TrackId, userId shared.UserId) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM tracks WHERE id = $1 AND user_id = $2`,
			id.UUID(), userId.UUID())
	})
}

func TestPgxTrackRepo_CountForUser_CountsOwnedRowsAndStopsAtTheBound(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	otherId := shared.NewUserId(uuid.New())

	for range 3 {
		seedTrackForDB(ctx, t, pool, userId)
	}
	seedTrackForDB(ctx, t, pool, otherId)

	whole, err := repo.CountForUser(ctx, userId, 10)
	if err != nil {
		t.Fatalf("CountForUser(10): %v", err)
	}
	if whole != 3 {
		t.Errorf("CountForUser(10) = %d, want 3 (another owner's track must not count)", whole)
	}

	bounded, err := repo.CountForUser(ctx, userId, 2)
	if err != nil {
		t.Fatalf("CountForUser(2): %v", err)
	}
	if bounded != 2 {
		t.Errorf("CountForUser(2) = %d, want 2: the count must stop at the bound", bounded)
	}
}

func TestPgxTrackRepo_AddAndGetByID(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)

	_, created, err := repo.Add(ctx, track)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !created {
		t.Fatal("Add() created = false, want true")
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetByID() returned nil, want track")
	}

	if got.ID.UUID() != track.ID.UUID() {
		t.Errorf("ID = %v, want %v", got.ID.UUID(), track.ID.UUID())
	}
	if got.UserId.UUID() != userId.UUID() {
		t.Errorf("UserId = %v, want %v", got.UserId.UUID(), userId.UUID())
	}
	if got.Title != track.Title {
		t.Errorf("Title = %q, want %q", got.Title, track.Title)
	}
	if got.Artist != track.Artist {
		t.Errorf("Artist = %q, want %q", got.Artist, track.Artist)
	}
	if got.Album != track.Album {
		t.Errorf("Album = %q, want %q", got.Album, track.Album)
	}
	if got.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("AcquisitionStatus = %v, want pending", got.AcquisitionStatus)
	}
	if got.DedupKey != track.DedupKey {
		t.Errorf("DedupKey = %q, want %q", got.DedupKey, track.DedupKey)
	}
}

func TestPgxTrackRepo_Add_DedupConflict(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track1 := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track1.ID, userId)

	_, created1, err := repo.Add(ctx, track1)
	if err != nil {
		t.Fatalf("first Add() error = %v", err)
	}
	if !created1 {
		t.Fatal("first Add() created = false, want true")
	}

	track2, err := domain.NewTrack(userId, track1.Title, track1.Artist, track1.Album)
	if err != nil {
		t.Fatalf("NewTrack for dedup: %v", err)
	}
	cleanupTrack(t, pool, track2.ID, userId)

	_, created2, err := repo.Add(ctx, track2)
	if err != nil {
		t.Fatalf("second Add() error = %v", err)
	}

	if created2 {
		t.Error("second Add() created = true, want false (dedup conflict)")
	}
}

func TestPgxTrackRepo_ListForUser(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	tracks := make([]*domain.Track, 3)
	for i := 0; i < 3; i++ {
		tracks[i] = newTestTrackForDB(t, userId)
		tracks[i].AddedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		cleanupTrack(t, pool, tracks[i].ID, userId)
		if _, _, err := repo.Add(ctx, tracks[i]); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
	}

	got, total, err := repo.ListForUser(ctx, userId, 2, 0)
	if err != nil {
		t.Fatalf("ListForUser() error = %v", err)
	}

	if total != 3 {
		t.Errorf("total = %d, want 3", total)
	}
	if len(got) != 2 {
		t.Errorf("len(tracks) = %d, want 2", len(got))
	}

	if len(got) >= 2 && got[0].AddedAt.Before(got[1].AddedAt) {
		t.Error("tracks not in descending added_at order")
	}

	got2, total2, err := repo.ListForUser(ctx, userId, 10, 2)
	if err != nil {
		t.Fatalf("ListForUser(offset=2) error = %v", err)
	}
	if total2 != 3 {
		t.Errorf("total at offset=2 = %d, want 3", total2)
	}
	if len(got2) != 1 {
		t.Errorf("len(tracks) at offset=2 = %d, want 1", len(got2))
	}
}

func TestPgxTrackRepo_Update(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)

	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	audioRef := "s3://bucket/test-" + uuid.New().String() + ".opus"
	if err := track.MarkReady(audioRef); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := repo.Update(ctx, track, track.Version); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID after update returned nil")
	}
	if got.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("AcquisitionStatus = %v, want ready", got.AcquisitionStatus)
	}
	if got.AudioRef == nil || *got.AudioRef != audioRef {
		t.Errorf("AudioRef = %v, want %q", got.AudioRef, audioRef)
	}
}

func TestPgxTrackRepo_UpdateDoesNotClobberConcurrentMetadataEdit(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	loaded, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID (acquisition read) error = %v", err)
	}
	if loaded == nil {
		t.Fatal("GetByID (acquisition read) returned nil")
	}

	const editedTitle = "Concurrently Retitled"
	const editedAlbum = "Concurrently Re-albumed"
	if _, err := pool.Exec(ctx,
		`UPDATE tracks SET title=$3, album=$4 WHERE id=$1 AND user_id=$2`,
		track.ID.UUID(), userId.UUID(), editedTitle, editedAlbum,
	); err != nil {
		t.Fatalf("concurrent metadata edit failed: %v", err)
	}

	audioRef := "s3://bucket/test-" + uuid.New().String() + ".opus"
	if err := loaded.MarkReady(audioRef); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || got == nil {
		t.Fatalf("GetByID after update: got=%v err=%v", got, err)
	}
	if got.Title != editedTitle {
		t.Errorf("Title = %q, want %q: the acquisition write clobbered a concurrent metadata edit (lost update)", got.Title, editedTitle)
	}
	if got.Album != editedAlbum {
		t.Errorf("Album = %q, want %q: the acquisition write clobbered a concurrent metadata edit (lost update)", got.Album, editedAlbum)
	}
	if got.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("AcquisitionStatus = %v, want ready", got.AcquisitionStatus)
	}
	if got.AudioRef == nil || *got.AudioRef != audioRef {
		t.Errorf("AudioRef = %v, want %q", got.AudioRef, audioRef)
	}
}

func TestPgxTrackRepo_Update_SameColumnCASMiss(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	writerA, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || writerA == nil {
		t.Fatalf("GetByID (writer A) got=%v err=%v", writerA, err)
	}
	writerB, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || writerB == nil {
		t.Fatalf("GetByID (writer B) got=%v err=%v", writerB, err)
	}
	if writerA.Version != writerB.Version {
		t.Fatalf("writers read different versions: A=%d B=%d", writerA.Version, writerB.Version)
	}

	refA := "s3://bucket/winner-" + uuid.New().String() + ".opus"
	if err := writerA.MarkReady(refA); err != nil {
		t.Fatalf("writer A MarkReady: %v", err)
	}
	if err := repo.Update(ctx, writerA, writerA.Version); err != nil {
		t.Fatalf("writer A Update() error = %v, want success", err)
	}
	if writerA.Version == writerB.Version {
		t.Errorf("winning Update did not advance the row version (still %d)", writerA.Version)
	}

	if err := writerB.MarkFailed("stale settle from a racing writer"); err != nil {
		t.Fatalf("writer B MarkFailed: %v", err)
	}
	err = repo.Update(ctx, writerB, writerB.Version)
	if !errors.Is(err, ports.ErrTrackVersionConflict) {
		t.Fatalf("writer B Update() error = %v, want ports.ErrTrackVersionConflict", err)
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || got == nil {
		t.Fatalf("GetByID after race: got=%v err=%v", got, err)
	}
	if got.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("AcquisitionStatus = %v, want ready: writer B's stale settle clobbered the winner", got.AcquisitionStatus)
	}
	if got.AudioRef == nil || *got.AudioRef != refA {
		t.Errorf("AudioRef = %v, want %q: writer B's stale settle clobbered the winner", got.AudioRef, refA)
	}
}

func TestPgxTrackRepo_Update_DeletedRowIsNotAConflict(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	loaded, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || loaded == nil {
		t.Fatalf("GetByID got=%v err=%v", loaded, err)
	}

	if _, _, err := repo.Delete(ctx, track.ID, userId); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if err := loaded.MarkReady("s3://bucket/gone-" + uuid.New().String() + ".opus"); err != nil {
		t.Fatalf("MarkReady: %v", err)
	}
	err = repo.Update(ctx, loaded, loaded.Version)
	if err == nil {
		t.Fatal("Update() on a deleted row returned nil, want an error")
	}
	if errors.Is(err, ports.ErrTrackVersionConflict) {
		t.Errorf("Update() on a deleted row = %v, want not-found (not a version conflict)", err)
	}
}

func TestPgxTrackRepo_Delete(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)

	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	deleted, _, err := repo.Delete(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if !deleted {
		t.Error("Delete() deleted = false, want true")
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("GetByID after delete: %v", err)
	}
	if got != nil {
		t.Errorf("GetByID after delete returned non-nil track: %v", got.ID)
	}

	deleted2, _, err := repo.Delete(ctx, track.ID, userId)
	if err != nil {
		t.Fatalf("second Delete() error = %v", err)
	}
	if deleted2 {
		t.Error("second Delete() deleted = true, want false (already gone)")
	}
}

func TestPgxTrackRepo_Delete_CrossTenantIDOR(t *testing.T) {
	pool := testPool(t)
	trackRepo := NewPgxTrackRepository(pool)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()

	userB := shared.NewUserId(uuid.New())
	userA := shared.NewUserId(uuid.New())

	trackB := newTestTrackForDB(t, userB)
	cleanupTrack(t, pool, trackB.ID, userB)
	if _, _, err := trackRepo.Add(ctx, trackB); err != nil {
		t.Fatalf("Add track B: %v", err)
	}

	plB := newTestPlaylistForDB(t, userB)
	cleanupPlaylist(t, pool, plB.ID, userB)
	if err := playlistRepo.Create(ctx, plB); err != nil {
		t.Fatalf("Create playlist B: %v", err)
	}
	if err := playlistRepo.AddTrack(ctx, userB, plB.ID, trackB.ID); err != nil {
		t.Fatalf("AddTrack B: %v", err)
	}

	deleted, _, err := trackRepo.Delete(ctx, trackB.ID, userA)
	if err != nil {
		t.Fatalf("Delete(trackB, userA) error = %v", err)
	}
	if deleted {
		t.Error("Delete(trackB, userA) deleted = true, want false (not owner)")
	}

	got, err := trackRepo.GetByID(ctx, trackB.ID, userB)
	if err != nil {
		t.Fatalf("GetByID after cross-tenant delete: %v", err)
	}
	if got == nil {
		t.Fatal("B's track vanished after A's delete attempt")
	}

	_, gotTracks, err := playlistRepo.GetWithTracks(ctx, plB.ID, userB)
	if err != nil {
		t.Fatalf("GetWithTracks after cross-tenant delete: %v", err)
	}
	if len(gotTracks) != 1 {
		t.Fatalf("B's playlist has %d tracks after A's delete, want 1", len(gotTracks))
	}
	if gotTracks[0].ID.UUID() != trackB.ID.UUID() {
		t.Errorf("B's playlist track = %v, want %v", gotTracks[0].ID.UUID(), trackB.ID.UUID())
	}
}

func rawPlaylistPositions(t *testing.T, pool *pgxpool.Pool, playlistId domain.PlaylistId) map[uuid.UUID]int {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT track_id, position FROM playlist_tracks WHERE playlist_id = $1`,
		playlistId.UUID())
	if err != nil {
		t.Fatalf("rawPlaylistPositions query: %v", err)
	}
	defer rows.Close()

	positions := map[uuid.UUID]int{}
	for rows.Next() {
		var (
			trackId uuid.UUID
			pos     int
		)
		if err := rows.Scan(&trackId, &pos); err != nil {
			t.Fatalf("rawPlaylistPositions scan: %v", err)
		}
		positions[trackId] = pos
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rawPlaylistPositions rows: %v", err)
	}
	return positions
}

func TestPgxTrackRepo_Delete_EvictsFromAllPlaylists(t *testing.T) {
	pool := testPool(t)
	trackRepo := NewPgxTrackRepository(pool)
	playlistRepo := NewPgxPlaylistRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	trackA := newTestTrackForDB(t, userId)
	trackB := newTestTrackForDB(t, userId)
	trackC := newTestTrackForDB(t, userId)
	for _, tr := range []*domain.Track{trackA, trackB, trackC} {
		cleanupTrack(t, pool, tr.ID, userId)
		if _, _, err := trackRepo.Add(ctx, tr); err != nil {
			t.Fatalf("Add track: %v", err)
		}
	}

	p1 := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, p1.ID, userId)
	if err := playlistRepo.Create(ctx, p1); err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	for _, tr := range []*domain.Track{trackA, trackB, trackC} {
		if err := playlistRepo.AddTrack(ctx, userId, p1.ID, tr.ID); err != nil {
			t.Fatalf("AddTrack p1: %v", err)
		}
	}

	p2 := newTestPlaylistForDB(t, userId)
	cleanupPlaylist(t, pool, p2.ID, userId)
	if err := playlistRepo.Create(ctx, p2); err != nil {
		t.Fatalf("Create p2: %v", err)
	}
	for _, tr := range []*domain.Track{trackB, trackC} {
		if err := playlistRepo.AddTrack(ctx, userId, p2.ID, tr.ID); err != nil {
			t.Fatalf("AddTrack p2: %v", err)
		}
	}

	deleted, _, err := trackRepo.Delete(ctx, trackB.ID, userId)
	if err != nil {
		t.Fatalf("Delete(trackB) error = %v", err)
	}
	if !deleted {
		t.Fatal("Delete(trackB) deleted = false, want true")
	}

	p1Pos := rawPlaylistPositions(t, pool, p1.ID)
	wantP1 := map[uuid.UUID]int{trackA.ID.UUID(): 0, trackC.ID.UUID(): 2}
	if _, present := p1Pos[trackB.ID.UUID()]; present {
		t.Error("trackB still present in p1 after delete")
	}
	for id, want := range wantP1 {
		if got, ok := p1Pos[id]; !ok || got != want {
			t.Errorf("p1 position for %v = %d (present=%v), want %d", id, got, ok, want)
		}
	}
	if len(p1Pos) != 2 {
		t.Errorf("p1 membership count = %d, want 2", len(p1Pos))
	}

	p2Pos := rawPlaylistPositions(t, pool, p2.ID)
	if _, present := p2Pos[trackB.ID.UUID()]; present {
		t.Error("trackB still present in p2 after delete")
	}
	if got, ok := p2Pos[trackC.ID.UUID()]; !ok || got != 1 {
		t.Errorf("p2 position for trackC = %d (present=%v), want 1", got, ok)
	}
	if len(p2Pos) != 1 {
		t.Errorf("p2 membership count = %d, want 1", len(p2Pos))
	}
}

func TestPgxTrackRepo_GetByID_NotFound(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	got, err := repo.GetByID(ctx, domain.TrackIdFromUUID(uuid.New()), userId)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got != nil {
		t.Errorf("GetByID() for missing track returned non-nil: %v", got.ID)
	}
}

func TestPgxTrackRepo_ListOwnedTrackRefs_BoundedByLimit(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	prev := maxOwnedTrackRefs
	maxOwnedTrackRefs = 3
	t.Cleanup(func() { maxOwnedTrackRefs = prev })

	const inserted = 5
	for i := 0; i < inserted; i++ {
		track := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, track.ID, userId)
		if _, _, err := repo.Add(ctx, track); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
	}

	refs, err := repo.ListOwnedTrackRefs(ctx, userId)
	if err != nil {
		t.Fatalf("ListOwnedTrackRefs() error = %v", err)
	}
	if len(refs) != maxOwnedTrackRefs {
		t.Fatalf("len(refs) = %d, want %d (bounded by limit, %d inserted)",
			len(refs), maxOwnedTrackRefs, inserted)
	}
}

func TestPgxTrackRepo_ListOwnedTrackRefs_PagesPastPageSize(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	prev := ownedTrackRefsPageSize
	ownedTrackRefsPageSize = 2
	t.Cleanup(func() { ownedTrackRefsPageSize = prev })

	const inserted = 5
	want := map[string]bool{}
	for i := 0; i < inserted; i++ {
		track := newTestTrackForDB(t, userId)
		cleanupTrack(t, pool, track.ID, userId)
		if _, _, err := repo.Add(ctx, track); err != nil {
			t.Fatalf("Add track %d: %v", i, err)
		}
		want[track.ID.String()] = true
	}

	refs, err := repo.ListOwnedTrackRefs(ctx, userId)
	if err != nil {
		t.Fatalf("ListOwnedTrackRefs() error = %v", err)
	}
	if len(refs) != inserted {
		t.Fatalf("len(refs) = %d, want %d (all tracks past the page size)", len(refs), inserted)
	}
	for _, ref := range refs {
		if !want[ref.ID] {
			t.Errorf("unexpected ref %s", ref.ID)
		}
	}
}

type countingPool struct {
	count   int
	err     error
	queries []string
}

func (p *countingPool) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected Begin")
}

func (p *countingPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (p *countingPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (p *countingPool) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	p.queries = append(p.queries, sql)
	return countRow{n: p.count, err: p.err}
}

type countRow struct {
	n   int
	err error
}

func (r countRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, d := range dest {
		ptr, ok := d.(*int)
		if !ok || ptr == nil {
			return fmt.Errorf("dest %T, want *int", d)
		}
		*ptr = r.n
	}
	return nil
}

func TestPageTotal_SkipsCountOnlyWhenPageProvesTotal(t *testing.T) {
	cases := []struct {
		name          string
		limit, offset int
		got, dbCount  int
		want          int
		wantCountSQL  bool
	}{
		{"short first page is the whole set", 50, 0, 7, 999, 7, false},
		{"short later page ends the set", 50, 100, 20, 999, 120, false},
		{"empty first page means zero", 50, 0, 0, 999, 0, false},
		{"full page needs a count", 50, 0, 50, 180, 180, true},
		{"empty page past offset needs a count", 50, 500, 0, 180, 180, true},
		{"count behind a concurrent add is clamped", 50, 50, 50, 90, 100, true},
	}
	for _, c := range cases {
		pool := &countingPool{count: c.dbCount}
		total, err := pageTotal(context.Background(), pool, c.limit, c.offset, c.got,
			`SELECT count(*) FROM tracks WHERE user_id = $1`, uuid.New())
		if err != nil {
			t.Fatalf("%s: pageTotal error = %v", c.name, err)
		}
		if total != c.want {
			t.Errorf("%s: total = %d, want %d", c.name, total, c.want)
		}
		if ran := len(pool.queries) == 1; ran != c.wantCountSQL {
			t.Errorf("%s: count query ran = %v (queries %v), want %v", c.name, ran, pool.queries, c.wantCountSQL)
		}
	}
}

func TestPageTotal_CountErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	pool := &countingPool{err: boom}
	_, err := pageTotal(context.Background(), pool, 10, 0, 10, `SELECT count(*) FROM tracks WHERE user_id = $1`, uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapping %v", err, boom)
	}
}

func TestPgxTrackRepo_ListForUser_TotalPastTheEnd(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})
	for i := 0; i < 3; i++ {
		tr := newTestTrackForDB(t, userId)
		if _, _, err := repo.Add(ctx, tr); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	got, total, err := repo.ListForUser(ctx, userId, 2, 10)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 0 || total != 3 {
		t.Fatalf("len=%d total=%d, want len=0 total=3", len(got), total)
	}
}

func keyed(t *testing.T, userId shared.UserId, key string) *domain.Track {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	track.IdempotencyKey = &key
	return track
}

func TestPgxTrackRepo_ConcurrentAddSameKey(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	key := "idem-" + uuid.NewString()

	const concurrent = 8
	tracks := make([]*domain.Track, concurrent)
	for i := range tracks {
		tr := keyed(t, userId, key)
		cleanupTrack(t, pool, tr.ID, userId)
		tracks[i] = tr
	}

	start := make(chan struct{})
	stored := make([]*domain.Track, concurrent)
	createdFlags := make([]bool, concurrent)
	errs := make([]error, concurrent)
	var wg sync.WaitGroup
	for i, tr := range tracks {
		wg.Add(1)
		go func(i int, tr *domain.Track) {
			defer wg.Done()
			<-start
			stored[i], createdFlags[i], errs[i] = repo.Add(ctx, tr)
		}(i, tr)
	}
	close(start)
	wg.Wait()

	createdCount := 0
	var winnerID domain.TrackId
	for i := range tracks {
		if errs[i] != nil {
			t.Fatalf("concurrent Add %d: %v", i, errs[i])
		}
		if stored[i] == nil {
			t.Fatalf("Add %d returned nil track", i)
		}
		if createdFlags[i] {
			createdCount++
			winnerID = stored[i].ID
		}
	}
	if createdCount != 1 {
		t.Fatalf("created count = %d, want exactly 1 (the key must collapse concurrent creates)", createdCount)
	}
	for i := range stored {
		if stored[i].ID != winnerID {
			t.Fatalf("Add %d returned id %s, want the single winning row %s", i, stored[i].ID, winnerID)
		}
	}

	if got := countTracksForUser(ctx, t, pool, userId); got != 1 {
		t.Fatalf("row count for user = %d, want 1", got)
	}
}

func TestPgxTrackRepo_AddSameKeyAfterCommit(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	key := "idem-" + uuid.NewString()

	first := keyed(t, userId, key)
	cleanupTrack(t, pool, first.ID, userId)
	firstStored, created, err := repo.Add(ctx, first)
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if !created {
		t.Fatalf("first Add created = false, want true")
	}

	retry := keyed(t, userId, key)
	cleanupTrack(t, pool, retry.ID, userId)
	retryStored, created, err := repo.Add(ctx, retry)
	if err != nil {
		t.Fatalf("retry Add: %v", err)
	}
	if created {
		t.Fatalf("retry Add created = true, want false (the committed row must be returned)")
	}
	if retryStored == nil || retryStored.ID != firstStored.ID {
		t.Fatalf("retry returned %v, want the first stored row %s", retryStored, firstStored.ID)
	}

	if got := countTracksForUser(ctx, t, pool, userId); got != 1 {
		t.Fatalf("row count for user = %d, want 1", got)
	}
}

func TestPgxTrackRepo_Update_RoundTripsAcquisitionConfidenceAndEvidence(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	before, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || before == nil {
		t.Fatalf("GetByID before: %v %v", before, err)
	}
	if before.ConfidenceScore != nil || before.EvidenceJSON != nil {
		t.Fatalf("fresh track carries confidence %v evidence %s", before.ConfidenceScore, before.EvidenceJSON)
	}

	evidence := `{"verdict": "hard", "acoustid_score": 0.9, "source_title": "Song"}`
	if err := track.SetAcquisitionConfidence(0.5, json.RawMessage(evidence)); err != nil {
		t.Fatalf("SetAcquisitionConfidence: %v", err)
	}
	if err := repo.Update(ctx, track, track.Version); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	got, err := repo.GetByID(ctx, track.ID, userId)
	if err != nil || got == nil {
		t.Fatalf("GetByID after: %v %v", got, err)
	}
	if got.AcquisitionConfidence() != 0.5 {
		t.Errorf("confidence = %v, want 0.5", got.AcquisitionConfidence())
	}
	var stored map[string]any
	if err := json.Unmarshal(got.AcquisitionEvidence(), &stored); err != nil {
		t.Fatalf("evidence not JSON: %v (%q)", err, got.AcquisitionEvidence())
	}
	if stored["verdict"] != "hard" || stored["source_title"] != "Song" {
		t.Errorf("evidence = %v", stored)
	}
}

func countTracksForUser(ctx context.Context, t *testing.T, pool *pgxpool.Pool, userId shared.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE user_id = $1`, userId.UUID()).Scan(&n); err != nil {
		t.Fatalf("count tracks: %v", err)
	}
	return n
}

func TestPgxTrackRepo_Add_PendingRowIsClaimableWithoutEnqueue(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	track := newTestTrackForDB(t, userId)
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := repo.Add(ctx, track); err != nil {
		t.Fatalf("Add: %v", err)
	}

	var claimable bool
	if err := pool.QueryRow(ctx,
		`SELECT acquisition_available_at <= now() FROM tracks WHERE id = $1`,
		track.ID.UUID()).Scan(&claimable); err != nil {
		t.Fatalf("read acquisition_available_at: %v", err)
	}
	if !claimable {
		t.Fatal("pending row has no acquisition_available_at, so no worker can ever claim it")
	}
}
