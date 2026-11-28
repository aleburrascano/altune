package downloadfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromlibrary/upgradestatetest"
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func insertUnqueuedTrack(t *testing.T, pool *pgxpool.Pool) *library.Track {
	t.Helper()
	track, err := librarytest.NewTrack(librarytest.NewTrackInput{UserId: ids.NewUserId(uuid.New()), Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours"}, time.Now, ids.Real)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending')`,
		track.ID().UUID(), track.UserId().UUID(), track.Title(), track.Artist(), track.Album(), uuid.NewString())
	if err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID().UUID())
	})
	return track
}

func reclaimTrack(t *testing.T, pool *pgxpool.Pool, trackID library.TrackId) download.ClaimNumber {
	t.Helper()
	var attempts int
	if err := pool.QueryRow(context.Background(),
		`UPDATE acquisition_jobs SET attempts = attempts + 1 WHERE track_id = $1 RETURNING attempts`,
		trackID.UUID()).Scan(&attempts); err != nil {
		t.Fatalf("reclaim: %v", err)
	}
	return download.ClaimNumber(attempts)
}

func rowState(t *testing.T, pool *pgxpool.Pool, trackID library.TrackId) (string, *string, *string) {
	t.Helper()
	var status string
	var audioRef, reason *string
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_status, audio_ref, failure_reason FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&status, &audioRef, &reason); err != nil {
		t.Fatalf("select row: %v", err)
	}
	return status, audioRef, reason
}

func TestTrackLedger_OnPostgresAReclaimedTrackRefusesTheOldOwnerAndAcceptsTheNew(t *testing.T) {
	pool := dbtest.Pool(t)
	ledger := downloadfromlibrary.NewLibraryTracks(library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics()), time.Now, upgradestatetest.New(), ids.Real)
	ctx := context.Background()
	track := insertUnqueuedTrack(t, pool)

	old := reclaimTrack(t, pool, track.ID())
	current := reclaimTrack(t, pool, track.ID())

	if err := ledger.Complete(ctx, track.ID(), track.UserId(), old, download.Completion{AudioRef: "old-owner-ref"}); !errors.Is(err, download.ErrLeaseLost) {
		t.Fatalf("old owner Complete = %v, want ErrLeaseLost", err)
	}
	if err := ledger.Fail(ctx, track.ID(), track.UserId(), old, "boom"); !errors.Is(err, download.ErrLeaseLost) {
		t.Fatalf("old owner Fail = %v, want ErrLeaseLost", err)
	}
	status, audioRef, reason := rowState(t, pool, track.ID())
	if status != "pending" || audioRef != nil || reason != nil {
		t.Fatalf("row = %s %v %v, want pending with no audio_ref and no failure_reason", status, audioRef, reason)
	}

	if err := ledger.Complete(ctx, track.ID(), track.UserId(), current, download.Completion{AudioRef: "new-owner-ref"}); err != nil {
		t.Fatalf("current owner Complete = %v, want nil", err)
	}
	status, audioRef, _ = rowState(t, pool, track.ID())
	if status != "ready" || audioRef == nil || *audioRef != "new-owner-ref" {
		t.Errorf("row = %s %v, want ready on new-owner-ref", status, audioRef)
	}
}

type claimDuringUpdateStore struct {
	*library.PostgresTrackRepository
	pool    *pgxpool.Pool
	t       *testing.T
	claimed bool
}

func (s *claimDuringUpdateStore) UpdateFenced(ctx context.Context, track *library.Track, expectedVersion, fence int) error {
	if !s.claimed {
		s.claimed = true
		reclaimTrack(s.t, s.pool, track.ID())
	}
	return s.PostgresTrackRepository.UpdateFenced(ctx, track, expectedVersion, fence)
}

func TestTrackLedger_OnPostgresAClaimBetweenReadAndUpdateFencesOutTheWriter(t *testing.T) {
	pool := dbtest.Pool(t)
	track := insertUnqueuedTrack(t, pool)
	held := reclaimTrack(t, pool, track.ID())
	store := &claimDuringUpdateStore{PostgresTrackRepository: library.NewPostgresTrackRepository(pool, librarytest.DiscardDBCallMetrics()), pool: pool, t: t}

	err := downloadfromlibrary.NewLibraryTracks(store, time.Now, upgradestatetest.New(), ids.Real).Complete(context.Background(), track.ID(), track.UserId(), held, download.Completion{AudioRef: "old-owner-ref"})

	if !errors.Is(err, download.ErrLeaseLost) {
		t.Fatalf("err = %v, want ErrLeaseLost from the fenced write", err)
	}
	status, audioRef, _ := rowState(t, pool, track.ID())
	if status != "pending" || audioRef != nil {
		t.Errorf("row = %s %v, want pending with no audio_ref", status, audioRef)
	}
}

type postgresLedger struct {
	*downloadfromlibrary.LibraryTracks
	store *library.PostgresTrackRepository
}

var downloadStatusOf = map[download.TrackStatus]library.DownloadStatus{
	download.TrackPending: library.AcquisitionPending,
	download.TrackReady:   library.AcquisitionReady,
	download.TrackFailed:  library.AcquisitionFailed,
}

func (l postgresLedger) AddTrack(t *testing.T, snapshot download.TrackSnapshot) {
	t.Helper()
	track, err := librarytest.NewTrack(librarytest.NewTrackInput{UserId: snapshot.UserId, Title: snapshot.Title, Artist: snapshot.Artist, Album: snapshot.Album}, time.Now, ids.Real)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	status, known := downloadStatusOf[snapshot.Status]
	if !known {
		t.Fatalf("no library status is mapped for download status %v", snapshot.Status)
	}
	librarytest.AmendTrack(track, func(s *librarytest.TrackSnapshot) {
		s.ID = snapshot.ID
		s.DownloadStatus = status
		s.AudioRef = snapshot.AudioRef
		s.DurationSeconds = snapshot.StoredDurationSeconds
	})
	if _, created, err := l.store.Add(context.Background(), track); err != nil || !created {
		t.Fatalf("Add = created %v, %v; want the track stored", created, err)
	}
	t.Cleanup(func() {
		if _, _, err := l.store.Delete(context.Background(), track.ID(), track.UserId()); err != nil {
			t.Errorf("cleanup Delete = %v", err)
		}
	})
}

func TestTrackLedger_OnPostgresMeetsTheLedgerContract(t *testing.T) {
	store := library.NewPostgresTrackRepository(dbtest.Pool(t), librarytest.DiscardDBCallMetrics())
	downloadtest.LedgerContract(t, func(*testing.T) downloadtest.LedgerUnderTest {
		return postgresLedger{LibraryTracks: downloadfromlibrary.NewLibraryTracks(store, time.Now, upgradestatetest.New(), ids.Real), store: store}
	})
}
