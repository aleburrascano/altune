package main

import (
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestLoadUnmatchedTracks_ReadsReadyTracksWithoutAnOfficialBinding(t *testing.T) {
	pool := sharedtest.Pool(t)
	ctx := context.Background()
	unbound, bound, user := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, acquisition_status, audio_ref) VALUES
		 ($1, $3, 'Unbound', 'Artist', gen_random_uuid()::text, 'ready', 'audio/unbound.mp3'),
		 ($2, $3, 'Bound', 'Artist', gen_random_uuid()::text, 'ready', 'audio/bound.mp3')`,
		unbound, bound, user); err != nil {
		t.Fatalf("seed tracks: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, user) })
	if _, err := pool.Exec(ctx,
		`INSERT INTO track_identity (track_id, user_id, status, reason, evidence, run_id, track_version_at_bind) VALUES
		 ($1, $3, 'unbound', 'test', '{}', $4, 1),
		 ($2, $3, 'bound', 'test', '{}', $4, 1)`,
		unbound, bound, user, uuid.New()); err != nil {
		t.Fatalf("seed identities: %v", err)
	}

	got, err := loadUnmatchedTracks(ctx, pool)

	if err != nil || !slices.ContainsFunc(got, func(u unmatchedTrack) bool { return u.ID == unbound.String() }) ||
		slices.ContainsFunc(got, func(u unmatchedTrack) bool { return u.ID == bound.String() }) {
		t.Fatalf("loadUnmatchedTracks = %d tracks, %v; want the unbound track and not the bound one", len(got), err)
	}
}
