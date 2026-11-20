package persistence_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func orphanRefFor(userId shared.UserId) string {
	return userId.String() + "/artist/album/title-" + uuid.NewString()[:8] + ".mp3"
}

func cleanupOrphan(t *testing.T, pool *pgxpool.Pool, audioRef string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM orphaned_audio WHERE audio_ref = $1`, audioRef)
	})
}

func addTrackAt(t *testing.T, pool *pgxpool.Pool, userId shared.UserId, audioRef string) {
	t.Helper()
	track := newTestTrackForDB(t, userId)
	if audioRef != "" {
		if err := track.MarkReady(audioRef); err != nil {
			t.Fatal(err)
		}
	}
	cleanupTrack(t, pool, track.ID, userId)
	if _, _, err := catalog.NewPgxTrackRepository(pool).Add(context.Background(), track); err != nil {
		t.Fatalf("Add: %v", err)
	}
}

func findOrphan(t *testing.T, repo *catalog.PgxOrphanedAudioRepository, audioRef string) (catalog.OrphanedAudio, bool) {
	t.Helper()
	all, err := repo.ListOrphanedAudio(context.Background(), 10_000)
	if err != nil {
		t.Fatalf("ListOrphanedAudio: %v", err)
	}
	for _, o := range all {
		if o.AudioRef == audioRef {
			return o, true
		}
	}
	return catalog.OrphanedAudio{}, false
}

func TestPgxOrphanedAudioRepo_RecordMarkResolve(t *testing.T) {
	pool := testPool(t)
	repo := catalog.NewPgxOrphanedAudioRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	trackId := catalog.NewTrackId()
	ref := orphanRefFor(userId)
	cleanupOrphan(t, pool, ref)

	orphan := catalog.OrphanedAudio{AudioRef: ref, UserId: userId, TrackId: trackId}
	if err := repo.RecordOrphanedAudio(ctx, orphan); err != nil {
		t.Fatalf("RecordOrphanedAudio: %v", err)
	}
	got, ok := findOrphan(t, repo, ref)
	if !ok || got.UserId != userId || got.TrackId != trackId || got.Attempts != 0 || got.RecordedAt.IsZero() {
		t.Fatalf("recorded orphan = %+v (found %v), want owner, track, zero attempts, a timestamp", got, ok)
	}

	if err := repo.MarkOrphanedAudioAttempt(ctx, ref, "storage down"); err != nil {
		t.Fatalf("MarkOrphanedAudioAttempt: %v", err)
	}
	if got, _ := findOrphan(t, repo, ref); got.Attempts != 1 {
		t.Errorf("attempts after one failure = %d, want 1", got.Attempts)
	}

	if err := repo.RecordOrphanedAudio(ctx, orphan); err != nil {
		t.Fatalf("re-record: %v", err)
	}
	if got, _ := findOrphan(t, repo, ref); got.Attempts != 0 {
		t.Errorf("attempts after re-record = %d, want reset to 0", got.Attempts)
	}

	if err := repo.ResolveOrphanedAudio(ctx, ref); err != nil {
		t.Fatalf("ResolveOrphanedAudio: %v", err)
	}
	if _, ok := findOrphan(t, repo, ref); ok {
		t.Error("resolved orphan still listed")
	}
}

func TestPgxOrphanedAudioRepo_ListPutsAttemptedLast(t *testing.T) {
	pool := testPool(t)
	repo := catalog.NewPgxOrphanedAudioRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	stuck, fresh := orphanRefFor(userId), orphanRefFor(userId)
	for _, ref := range []string{stuck, fresh} {
		cleanupOrphan(t, pool, ref)
		if err := repo.RecordOrphanedAudio(ctx, catalog.OrphanedAudio{AudioRef: ref, UserId: userId, TrackId: catalog.NewTrackId()}); err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.MarkOrphanedAudioAttempt(ctx, stuck, "storage down"); err != nil {
		t.Fatal(err)
	}
	all, err := repo.ListOrphanedAudio(ctx, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	pos := map[string]int{}
	for i, o := range all {
		pos[o.AudioRef] = i
	}
	if pos[fresh] > pos[stuck] {
		t.Errorf("never-attempted orphan listed at %d after the attempted one at %d", pos[fresh], pos[stuck])
	}
}

func TestPgxOrphanedAudioRepo_AudioUsage(t *testing.T) {
	pool := testPool(t)
	repo := catalog.NewPgxOrphanedAudioRepository(pool)
	ctx := context.Background()

	usage := func(ref string, owner shared.UserId) catalog.AudioUsage {
		t.Helper()
		got, err := repo.AudioUsage(ctx, ref, owner)
		if err != nil {
			t.Fatalf("AudioUsage: %v", err)
		}
		return got
	}

	owner := shared.NewUserId(uuid.New())
	ref := orphanRefFor(owner)
	if got := usage(ref, owner); got != catalog.AudioUnused {
		t.Errorf("unreferenced key usage = %v, want unused", got)
	}

	other := shared.NewUserId(uuid.New())
	addTrackAt(t, pool, other, ref)
	if got := usage(ref, owner); got != catalog.AudioReferenced {
		t.Errorf("key referenced by another user's track usage = %v, want referenced", got)
	}

	busyOwner := shared.NewUserId(uuid.New())
	busyRef := orphanRefFor(busyOwner)
	addTrackAt(t, pool, busyOwner, "")
	if got := usage(busyRef, busyOwner); got != catalog.AudioOwnerAcquiring {
		t.Errorf("owner with a pending acquisition usage = %v, want owner acquiring", got)
	}
	if got := usage(busyRef, owner); got != catalog.AudioUnused {
		t.Errorf("another user's pending acquisition blocked the key: usage = %v, want unused", got)
	}

	replaceRef := strings.TrimSuffix(ref, ".mp3") + ".replace-" + uuid.NewString() + ".mp3"
	if got := usage(replaceRef, owner); got != catalog.AudioUnused {
		t.Errorf("replace key usage = %v, want unused (only the canonical key is referenced)", got)
	}
}

func TestPgxOrphanedAudioRepo_MissingTableIsUnavailable(t *testing.T) {
	admin := testPool(t)
	ctx := context.Background()
	schema := "orphan_nomig_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), `DROP SCHEMA `+schema+` CASCADE`) })

	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	repo := catalog.NewPgxOrphanedAudioRepository(pool)

	userId := shared.NewUserId(uuid.New())
	orphan := catalog.OrphanedAudio{AudioRef: orphanRefFor(userId), UserId: userId, TrackId: catalog.NewTrackId()}
	_, listErr := repo.ListOrphanedAudio(ctx, 10)
	for name, err := range map[string]error{
		"record":  repo.RecordOrphanedAudio(ctx, orphan),
		"list":    listErr,
		"resolve": repo.ResolveOrphanedAudio(ctx, orphan.AudioRef),
		"mark":    repo.MarkOrphanedAudioAttempt(ctx, orphan.AudioRef, "x"),
	} {
		if !errors.Is(err, catalog.ErrOrphanedAudioQueueUnavailable) {
			t.Errorf("%s err = %v, want ErrOrphanedAudioQueueUnavailable", name, err)
		}
	}
}
