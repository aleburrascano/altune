package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"reflect"
	"testing"
	"time"
)

func TestUnreleasedStore_SongsRoundTripEveryField(t *testing.T) {
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresUnreleasedStore(pool)
	ctx := context.Background()
	hasMedia := true
	cases := map[string]domain.UnreleasedSong{
		"every field": {
			GeniusID:     42,
			Title:        "Lost Song",
			CoverURL:     "https://img.example/c.jpg",
			GeniusURL:    "https://genius.com/lost-song",
			ReleaseDate:  "2024-05-01",
			HasMedia:     &hasMedia,
			Quality:      "snippet",
			TrackerKey:   "tracker-1",
			AudioLinks:   []domain.AudioLink{{Provider: domain.AudioProviderSoundCloud, ExternalID: "sc1", URL: "https://soundcloud.com/a/b"}},
			OpenURL:      "https://open.example/x",
			OpenHost:     "open.example",
			OffStreaming: true,
		},
		"unknown media": {GeniusID: 1, Title: "Bare", GeniusURL: "https://genius.com/bare"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			key := newUnreleasedArtist(t, pool, store)
			if err := store.Write(ctx, key, []domain.UnreleasedSong{want}, true, time.Now().UTC(), 1); err != nil {
				t.Fatalf("Write: %v", err)
			}
			songs, _, _, _, err := store.Read(ctx, key)
			if err != nil {
				t.Fatalf("Read: %v", err)
			}
			if len(songs) != 1 || !reflect.DeepEqual(songs[0], want) {
				t.Errorf("round trip = %+v, want [%+v]", songs, want)
			}
		})
	}
}

func TestUnreleasedStore_DueReturnsTheRecordedKnownIDs(t *testing.T) {
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresUnreleasedStore(pool)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `UPDATE unreleased_artists SET last_requested_at = now() - interval '60 days'`); err != nil {
		t.Fatalf("park existing artists: %v", err)
	}
	key := newUnreleasedArtist(t, pool, store)
	want := map[domain.ProviderName]string{domain.ProviderDeezer: "d1", domain.ProviderMusicBrainz: "mb1"}
	if err := store.Record(ctx, key, "Known IDs", want); err != nil {
		t.Fatalf("Record: %v", err)
	}

	due, err := store.Due(ctx, time.Now().UTC(), 10)
	if err != nil {
		t.Fatalf("Due: %v", err)
	}
	if len(due) != 1 || due[0].Key != key || !reflect.DeepEqual(due[0].KnownIDs, want) {
		t.Errorf("Due = %+v, want only %v with known IDs %v", due, key, want)
	}
}
