package main

import (
	"altune/go-api/internal/shared/songfacts"
	"context"
	"errors"
	"testing"
)

type fakeDeezer struct {
	byISRC  map[string]deezerTrack
	byID    map[string]deezerTrack
	albums  map[string]deezerAlbum
	search  map[string][]deezerAlbum
	blocked bool
	calls   int
}

func (f *fakeDeezer) TrackByISRC(_ context.Context, isrc string) (deezerTrack, error) {
	f.calls++
	if f.blocked {
		return deezerTrack{}, blocked("HTTP 403")
	}
	track, ok := f.byISRC[songfacts.CanonicalISRC(isrc)]
	if !ok {
		return deezerTrack{}, errCatalogUnknown
	}
	return track, nil
}

func (f *fakeDeezer) TrackByID(_ context.Context, id string) (deezerTrack, error) {
	f.calls++
	track, ok := f.byID[id]
	if !ok {
		return deezerTrack{}, errCatalogUnknown
	}
	return track, nil
}

func (f *fakeDeezer) SearchAlbums(_ context.Context, _, title string) ([]deezerAlbum, error) {
	f.calls++
	return f.search[title], nil
}

func (f *fakeDeezer) Album(_ context.Context, id string) (deezerAlbum, error) {
	f.calls++
	album, ok := f.albums[id]
	if !ok {
		return deezerAlbum{}, errCatalogUnknown
	}
	return album, nil
}

const (
	celebrationISRC  = "USPR38007190"
	compilationTrack = "9001"
	celebrateTrack   = "9002"
)

func celebrationDeezer() *fakeDeezer {
	return &fakeDeezer{
		byISRC: map[string]deezerTrack{
			celebrationISRC: {ID: compilationTrack, ISRC: celebrationISRC, Title: "Celebration", Position: 4, DurationSeconds: 221, AlbumID: "70", AlbumTitle: "Greatest Hits of the 80's Vol 3"},
		},
		byID: map[string]deezerTrack{
			celebrateTrack: {ID: celebrateTrack, ISRC: celebrationISRC, Title: "Celebration", Position: 1, DurationSeconds: 298},
		},
		albums: map[string]deezerAlbum{
			"70": {ID: "70", Title: "Greatest Hits of the 80's Vol 3", CoverURL: "https://cdn/comp.jpg", Genre: "Pop"},
			"80": {ID: "80", Title: "Celebrate!", CoverURL: "https://cdn/celebrate.jpg", Genre: "R&B", Tracks: []deezerAlbumTrack{{ID: "9000", Title: "Celebration (Part 2)"}, {ID: celebrateTrack, Title: "Celebration"}}},
		},
		search: map[string][]deezerAlbum{
			"Celebrate!": {{ID: "81", Title: "Celebrate! (Remastered Live)"}, {ID: "80", Title: "Celebrate!"}},
		},
	}
}

func celebrationInput() crossCheckInput {
	return crossCheckInput{
		ISRC: celebrationISRC, Artist: "Kool & The Gang", SongTitle: "Celebration",
		Group: group("g", "Celebrate!", "Album", "1980-10"), RecordingISRCs: []string{celebrationISRC},
	}
}

func TestCrossCheck_TakesTheFieldsFromTheAlbumTheIsrcLookupNames(t *testing.T) {
	dz := celebrationDeezer()
	in := celebrationInput()
	in.Group.Title = "Greatest Hits of the 80's Vol 3"
	got, err := crossChecker{deezer: dz}.check(t.Context(), in)
	if err != nil || got.Outcome != outcomeSameISRC || got.Match.AlbumID != "70" || got.Match.TrackPosition != 4 || got.Match.Genre != "Pop" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCrossCheck_FindsTheChosenAlbumWhenTheIsrcLookupAnswersWithACompilation(t *testing.T) {
	got, err := crossChecker{deezer: celebrationDeezer()}.check(t.Context(), celebrationInput())
	want := deezerMatch{AlbumID: "80", AlbumTitle: "Celebrate!", CoverURL: "https://cdn/celebrate.jpg", Genre: "R&B", TrackPosition: 1, DurationSeconds: 298, ISRC: celebrationISRC}
	if err != nil || got.Outcome != outcomeSameISRC || got.Match == nil || *got.Match != want {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCrossCheck_AnAlbumOfTheRightTitleWithoutTheIsrcIsNotAccepted(t *testing.T) {
	dz := celebrationDeezer()
	dz.byID[celebrateTrack] = deezerTrack{ID: celebrateTrack, ISRC: "USXXX0000000", Title: "Celebration", DurationSeconds: 298}
	got, err := crossChecker{deezer: dz}.check(t.Context(), celebrationInput())
	if err != nil || got.Outcome != outcomeNoAlbum || got.Match != nil || got.OtherMaster != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCrossCheck_ADifferentIsrcThatMusicBrainzListsForTheRecordingIsAnotherMaster(t *testing.T) {
	dz := celebrationDeezer()
	dz.byID[celebrateTrack] = deezerTrack{ID: celebrateTrack, ISRC: "USPR18000001", Title: "Celebration", DurationSeconds: 298}
	in := celebrationInput()
	in.RecordingISRCs = []string{celebrationISRC, "USPR18000001"}
	got, err := crossChecker{deezer: dz}.check(t.Context(), in)
	if err != nil || got.Outcome != outcomeOtherMaster || got.OtherMaster.ISRC != "USPR18000001" || got.OtherMaster.AlbumTitle != "Celebrate!" || got.Match != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCrossCheck_NoAlbumAnywhereIsNoFill(t *testing.T) {
	got, err := crossChecker{deezer: &fakeDeezer{}}.check(t.Context(), celebrationInput())
	if err != nil || got.Outcome != outcomeNoAlbum || got.Match != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestCrossCheck_ABlockedDeezerStopsTheCheck(t *testing.T) {
	_, err := crossChecker{deezer: &fakeDeezer{blocked: true}}.check(t.Context(), celebrationInput())
	if !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestCrossCheck_AByIsrcTrackWithAnotherIsrcIsNotAccepted(t *testing.T) {
	dz := celebrationDeezer()
	dz.byISRC[celebrationISRC] = deezerTrack{ID: compilationTrack, ISRC: "USXXX0000000", Title: "Celebration", Position: 4, DurationSeconds: 221, AlbumID: "70", AlbumTitle: "Celebrate!"}
	got, err := crossChecker{deezer: dz}.check(t.Context(), celebrationInput())
	if err != nil || got.Outcome != outcomeSameISRC || got.Match.AlbumID != "80" {
		t.Fatalf("got %+v, %v", got, err)
	}
}
