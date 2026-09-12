package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
)

func TestAddTrackService_ValidatesRanges(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	negDuration := -100.0
	zeroTrackNumber := 0
	negTrackNumber := -3
	farFutureYear := time.Now().UTC().Year() + 50
	ancientYear := 1000

	tests := []struct {
		name    string
		mutate  func(*AddTrackInput)
		wantErr string
	}{
		{
			name:    "negative duration is rejected",
			mutate:  func(in *AddTrackInput) { in.DurationSeconds = &negDuration },
			wantErr: "duration_seconds",
		},
		{
			name:    "zero track number is rejected",
			mutate:  func(in *AddTrackInput) { in.TrackNumber = &zeroTrackNumber },
			wantErr: "track_number",
		},
		{
			name:    "negative track number is rejected",
			mutate:  func(in *AddTrackInput) { in.TrackNumber = &negTrackNumber },
			wantErr: "track_number",
		},
		{
			name:    "far-future year is rejected",
			mutate:  func(in *AddTrackInput) { in.Year = &farFutureYear },
			wantErr: "year",
		},
		{
			name:    "ancient year is rejected",
			mutate:  func(in *AddTrackInput) { in.Year = &ancientYear },
			wantErr: "year",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			svc := NewAddTrackService(repo)
			input := AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"}
			tt.mutate(&input)

			out, err := svc.Execute(ctx, userId, input)

			if err == nil {
				t.Fatalf("expected a validation error, got nil (out=%+v)", out)
			}
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error = %T (%v), want *domain.ValidationError", err, err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err.Error(), tt.wantErr)
			}
			if len(repo.Tracks) != 0 {
				t.Fatalf("stored %d tracks, want the invalid track rejected before persistence", len(repo.Tracks))
			}
		})
	}
}

func TestAddTrackService_ValidatesFreeFormFields(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	oversized := strings.Repeat("x", 301)
	malformedURL := "not a url"
	schemelessURL := "example.com/song.mp3"
	ftpURL := "ftp://example.com/song.mp3"

	tests := []struct {
		name    string
		mutate  func(*AddTrackInput)
		wantErr string
	}{
		{
			name:    "oversized artwork_url is rejected",
			mutate:  func(in *AddTrackInput) { in.ArtworkURL = &oversized },
			wantErr: "artwork_url",
		},
		{
			name:    "oversized genre is rejected",
			mutate:  func(in *AddTrackInput) { in.Genre = &oversized },
			wantErr: "genre",
		},
		{
			name:    "oversized album_artist is rejected",
			mutate:  func(in *AddTrackInput) { in.AlbumArtist = &oversized },
			wantErr: "album_artist",
		},
		{
			name:    "oversized isrc is rejected",
			mutate:  func(in *AddTrackInput) { in.ISRC = &oversized },
			wantErr: "isrc",
		},
		{
			name:    "malformed source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &malformedURL },
			wantErr: "source_url",
		},
		{
			name:    "schemeless source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &schemelessURL },
			wantErr: "source_url",
		},
		{
			name:    "non-http source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &ftpURL },
			wantErr: "source_url",
		},
		{
			name:    "oversized source_url is rejected",
			mutate:  func(in *AddTrackInput) { in.SourceURL = &oversized },
			wantErr: "source_url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			svc := NewAddTrackService(repo)
			input := AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"}
			tt.mutate(&input)

			out, err := svc.Execute(ctx, userId, input)

			if err == nil {
				t.Fatalf("expected a validation error, got nil (out=%+v)", out)
			}
			var ve *domain.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("error = %T (%v), want *domain.ValidationError", err, err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want it to mention %q", err.Error(), tt.wantErr)
			}
			if len(repo.Tracks) != 0 {
				t.Fatalf("stored %d tracks, want the invalid track rejected before persistence", len(repo.Tracks))
			}
		})
	}
}

func TestAddTrackService_AcceptsValidFreeFormFields(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	artwork := "https://cdn.example.com/art.jpg"
	genre := "Jazz"
	albumArtist := "Various Artists"
	isrc := "USUM71703861"
	sourceURL := "https://example.com/song.mp3"

	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)
	input := AddTrackInput{
		Title:       "Track",
		Artist:      "Artist",
		Album:       "Album",
		ArtworkURL:  &artwork,
		Genre:       &genre,
		AlbumArtist: &albumArtist,
		ISRC:        &isrc,
		SourceURL:   &sourceURL,
	}

	out, err := svc.Execute(ctx, userId, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Track.ArtworkURL == nil || *out.Track.ArtworkURL != artwork {
		t.Errorf("ArtworkURL = %v, want %v", out.Track.ArtworkURL, artwork)
	}
	if out.Track.ISRC == nil || *out.Track.ISRC != isrc {
		t.Errorf("ISRC = %v, want %v", out.Track.ISRC, isrc)
	}
}

func TestAddTrackService_AcceptsValidRanges(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	duration := 210.5
	trackNumber := 7
	year := time.Now().UTC().Year()

	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)
	input := AddTrackInput{
		Title:           "Track",
		Artist:          "Artist",
		Album:           "Album",
		DurationSeconds: &duration,
		TrackNumber:     &trackNumber,
		Year:            &year,
	}

	out, err := svc.Execute(ctx, userId, input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Track.DurationSeconds == nil || *out.Track.DurationSeconds != duration {
		t.Errorf("DurationSeconds = %v, want %v", out.Track.DurationSeconds, duration)
	}
	if out.Track.TrackNumber == nil || *out.Track.TrackNumber != trackNumber {
		t.Errorf("TrackNumber = %v, want %v", out.Track.TrackNumber, trackNumber)
	}
	if out.Track.Year == nil || *out.Track.Year != year {
		t.Errorf("Year = %v, want %v", out.Track.Year, year)
	}
}
