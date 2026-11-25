package playbackcatalog_test

import (
	"altune/go-api/internal/app/playbackcatalog"
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/playback"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

type catalogTracksFunc func(id catalog.TrackId, userId shared.UserId) (*catalog.Track, error)

func (f catalogTracksFunc) GetByID(_ context.Context, id catalog.TrackId, userId shared.UserId) (*catalog.Track, error) {
	return f(id, userId)
}

func TestNowPlayingTracks_MapsTheUsersCatalogTrack(t *testing.T) {
	user := shared.NewUserId(uuid.New())
	id := catalog.NewTrackId()
	artwork, duration := "https://img.example/a.jpg", 212.5
	var askedFor catalog.TrackId
	var askedBy shared.UserId
	source := playbackcatalog.NewNowPlayingTracks(catalogTracksFunc(func(trackId catalog.TrackId, userId shared.UserId) (*catalog.Track, error) {
		askedFor, askedBy = trackId, userId
		return &catalog.Track{
			ID: id, Title: "Track Y", Artist: "Artist", ArtworkURL: &artwork, DurationSeconds: &duration,
			AcquisitionStatus: catalog.AcquisitionReady,
		}, nil
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
	if got == nil || *got != want {
		t.Errorf("NowPlayingTrack = %+v, want %+v", got, want)
	}
}

func TestNowPlayingTracks_AbsentTrackIsNotFound(t *testing.T) {
	source := playbackcatalog.NewNowPlayingTracks(catalogTracksFunc(func(catalog.TrackId, shared.UserId) (*catalog.Track, error) { return nil, catalog.ErrTrackNotFound }))

	got, err := source.NowPlayingTrack(context.Background(), shared.NewUserId(uuid.New()), catalog.NewTrackId())
	if got != nil || !errors.Is(err, playback.ErrNowPlayingTrackNotFound) {
		t.Errorf("NowPlayingTrack = (%+v, %v), want no track and ErrNowPlayingTrackNotFound", got, err)
	}
}

func TestNowPlayingTracks_TransientCatalogFailureIsASourceOutage(t *testing.T) {
	transient := fmt.Errorf("get track: %w", catalog.ErrDBTransient)
	source := playbackcatalog.NewNowPlayingTracks(catalogTracksFunc(func(catalog.TrackId, shared.UserId) (*catalog.Track, error) { return nil, transient }))

	_, err := source.NowPlayingTrack(context.Background(), shared.NewUserId(uuid.New()), catalog.NewTrackId())
	if !errors.Is(err, playback.ErrTrackSourceUnavailable) || !errors.Is(err, catalog.ErrDBTransient) {
		t.Errorf("err = %v, want it to be both ErrTrackSourceUnavailable and catalog's ErrDBTransient", err)
	}
}

func TestNowPlayingTracks_RowErrorIsNotASourceOutage(t *testing.T) {
	rowErr := errors.New("acquisition status no longer parses")
	source := playbackcatalog.NewNowPlayingTracks(catalogTracksFunc(func(catalog.TrackId, shared.UserId) (*catalog.Track, error) { return nil, rowErr }))

	_, err := source.NowPlayingTrack(context.Background(), shared.NewUserId(uuid.New()), catalog.NewTrackId())
	if !errors.Is(err, rowErr) || errors.Is(err, playback.ErrTrackSourceUnavailable) {
		t.Errorf("err = %v, want the row error itself, not a source outage", err)
	}
}
