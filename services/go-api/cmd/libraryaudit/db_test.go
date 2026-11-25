package main

import (
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestLoadReadyTracks_ReadsOnlyTheUsersReadyTracks(t *testing.T) {
	pool := sharedtest.Pool(t)
	ctx := context.Background()
	ready, pending, user := uuid.New(), uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, acquisition_status, audio_ref) VALUES
		 ($1, $3, 'Ready', 'Artist', gen_random_uuid()::text, 'ready', 'audio/ready.mp3'),
		 ($2, $3, 'Pending', 'Artist', gen_random_uuid()::text, 'pending', NULL)`,
		ready, pending, user); err != nil {
		t.Fatalf("seed tracks: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, user) })

	got, err := loadReadyTracks(ctx, pool, user.String(), 0)

	if err != nil || len(got) != 1 || got[0].ID != ready.String() || got[0].Title != "Ready" {
		t.Fatalf("loadReadyTracks = %+v, %v; want only the ready track", got, err)
	}
}
