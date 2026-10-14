package main

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestStore_LoadHeldIndexesEveryReadyTrackOfTheLibraryByArtist(t *testing.T) {
	s := testStore(t)
	userID := uuid.NewString()
	first := seedReady(t, s, userID)
	other := seedReady(t, s, userID)
	if _, err := s.pool.Exec(context.Background(), "UPDATE tracks SET title = 'Jump', album = 'Greatest' WHERE id = $1", other.ID); err != nil {
		t.Fatal(err)
	}
	held, err := s.loadHeld(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	owned := held.holdingsFor(first)
	if len(owned) != 1 || owned[0] != (heldKeys{Album: "greatest", Title: "jump"}) {
		t.Errorf("holdings = %+v", owned)
	}
}
