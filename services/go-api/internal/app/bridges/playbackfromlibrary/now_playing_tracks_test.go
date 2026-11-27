package playbackfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/playbackfromlibrary"
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/playback"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

type catalogTracksFunc func(id library.TrackId, userId ids.UserId) (*library.Track, error)

func (f catalogTracksFunc) GetByID(_ context.Context, id library.TrackId, userId ids.UserId) (*library.Track, error) {
	return f(id, userId)
}

func TestNowPlayingTracks_MapsTheUsersCatalogTrack(t *testing.T) {
	user := ids.NewUserId(uuid.New())
	id := librarytest.NewTrackId()
	artwork, duration := "https://img.example/a.jpg", 212.5
	var askedFor library.TrackId
	var askedBy ids.UserId
	source := playbackfromlibrary.NewNowPlayingTracks(catalogTracksFunc(func(trackId library.TrackId, userId ids.UserId) (*library.Track, error) {
		askedFor, askedBy = trackId, userId
		return librarytest.RestoreTrack(librarytest.TrackSnapshot{
			ID: id, Title: "Track Y", Artist: "Artist", ArtworkURL: &artwork, DurationSeconds: &duration,
			DownloadStatus: library.AcquisitionReady,
		}), nil
	}))

	got, err := source.NowPlayingTrack(context.Background(), user, id)
	if err != nil {
		t.Fatalf("NowPlayingTrack: %v", err)
	}
	if askedFor != id || askedBy != user {
		t.Errorf("catalog asked for (%v, %v), want (%v, %v)", askedFor, askedBy, id, user)
	}
	want := playback.NowPlayingTrack{
		Id: id.String(), Title: "Track Y", Artist: "Artist", ArtworkURL: &artwork, DurationSeconds: &duration,
		AcquisitionStatus: "ready",
	}
	if got == nil || got.Id != want.Id || got.Title != want.Title || got.Artist != want.Artist ||
		got.AcquisitionStatus != want.AcquisitionStatus ||
		got.ArtworkURL == nil || *got.ArtworkURL != artwork ||
		got.DurationSeconds == nil || *got.DurationSeconds != duration {
		t.Errorf("NowPlayingTrack = %+v, want %+v", got, want)
	}
}

func TestNowPlayingTracks_AbsentTrackIsNotFound(t *testing.T) {
	source := playbackfromlibrary.NewNowPlayingTracks(catalogTracksFunc(func(library.TrackId, ids.UserId) (*library.Track, error) { return nil, library.ErrTrackNotFound }))

	got, err := source.NowPlayingTrack(context.Background(), ids.NewUserId(uuid.New()), librarytest.NewTrackId())
	if got != nil || !errors.Is(err, playback.ErrNowPlayingTrackNotFound) {
		t.Errorf("NowPlayingTrack = (%+v, %v), want no track and ErrNowPlayingTrackNotFound", got, err)
	}
}

func TestNowPlayingTracks_TransientCatalogFailureIsASourceOutage(t *testing.T) {
	transient := fmt.Errorf("get track: %w", library.ErrDBTransient)
	source := playbackfromlibrary.NewNowPlayingTracks(catalogTracksFunc(func(library.TrackId, ids.UserId) (*library.Track, error) { return nil, transient }))

	_, err := source.NowPlayingTrack(context.Background(), ids.NewUserId(uuid.New()), librarytest.NewTrackId())
	if !errors.Is(err, playback.ErrTrackSourceUnavailable) || !errors.Is(err, library.ErrDBTransient) {
		t.Errorf("err = %v, want it to be both ErrTrackSourceUnavailable and catalog's ErrDBTransient", err)
	}
}

func TestNowPlayingTracks_RowErrorIsNotASourceOutage(t *testing.T) {
	rowErr := errors.New("acquisition status no longer parses")
	source := playbackfromlibrary.NewNowPlayingTracks(catalogTracksFunc(func(library.TrackId, ids.UserId) (*library.Track, error) { return nil, rowErr }))

	_, err := source.NowPlayingTrack(context.Background(), ids.NewUserId(uuid.New()), librarytest.NewTrackId())
	if !errors.Is(err, rowErr) || errors.Is(err, playback.ErrTrackSourceUnavailable) {
		t.Errorf("err = %v, want the row error itself, not a source outage", err)
	}
}
