package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func strptr(s string) *string { return &s }

// withTrackCap lowers the per-user library cap for one test, so crossing it
// costs a handful of rows rather than fifty thousand.
func withTrackCap(t *testing.T, limit int) {
	t.Helper()
	prev := maxTracksPerUser
	maxTracksPerUser = limit
	t.Cleanup(func() { maxTracksPerUser = prev })
}

// TestAddTrack_RejectsPastUserCap reproduces #2200: distinct titles bypass
// dedup, so nothing stopped one account from inserting tracks without limit.
// The save that would cross the cap is refused, and stores nothing.
func TestAddTrack_RejectsPastUserCap(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	withTrackCap(t, 3)
	for i := range maxTracksPerUser {
		seedTrack(t, repo, userId, fmt.Sprintf("Held %d", i), "Artist", "Album")
	}
	svc := NewAddTrackService(repo)

	out, err := svc.Execute(ctx, userId, AddTrackInput{Title: "One Too Many", Artist: "Artist", Album: "Album"})

	if !errors.Is(err, ErrLibraryFull) {
		t.Fatalf("error = %v, want ErrLibraryFull", err)
	}
	if out != nil {
		t.Fatalf("output = %+v, want nil", out)
	}
	if len(repo.Tracks) != maxTracksPerUser {
		t.Fatalf("stored tracks = %d, want %d: the refused save must not insert", len(repo.Tracks), maxTracksPerUser)
	}
}

// The cap counts the caller's own rows: a library full next door may not
// refuse this account's save.
func TestAddTrack_CapCountsOnlyTheCallersTracks(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	withTrackCap(t, 2)
	for i := range maxTracksPerUser {
		seedTrack(t, repo, testOtherUserId(), fmt.Sprintf("Theirs %d", i), "Artist", "Album")
	}
	svc := NewAddTrackService(repo)

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Mine", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !out.Created {
		t.Fatal("Created = false, want true: another owner's rows are not this caller's cap")
	}
}

// A second save carrying the same idempotency key must return the first stored
// track (created=false), even when its content differs — the key, not the
// content, decides identity here.
func TestAddTrackService_IdempotencyKeyReturnsExisting(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)
	key := "save-abc-123"

	first, err := svc.Execute(ctx, userId, AddTrackInput{
		Title: "One", Artist: "Artist", Album: "Album", IdempotencyKey: strptr(key),
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	if !first.Created {
		t.Fatalf("first Created = false, want true")
	}

	second, err := svc.Execute(ctx, userId, AddTrackInput{
		Title: "Different Title", Artist: "Other", Album: "Other", IdempotencyKey: strptr(key),
	})
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	if second.Created {
		t.Fatalf("second Created = true, want false (same key must collapse)")
	}
	if second.Track.ID != first.Track.ID {
		t.Fatalf("second track id = %s, want first %s", second.Track.ID, first.Track.ID)
	}
	if len(repo.Tracks) != 1 {
		t.Fatalf("stored tracks = %d, want 1", len(repo.Tracks))
	}
}

func TestAddTrackService_RejectsEmptyIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	svc := NewAddTrackService(repo)

	_, err := svc.Execute(ctx, testUserId(), AddTrackInput{
		Title: "T", Artist: "A", Album: "Al", IdempotencyKey: strptr(""),
	})
	if err == nil || !strings.Contains(err.Error(), "idempotency_key must not be empty") {
		t.Fatalf("err = %v, want empty idempotency_key validation error", err)
	}
}

func TestAddTrackService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	errRepo := errors.New("db connection lost")

	tests := []struct {
		name        string
		input       AddTrackInput
		setup       func(*catalogtest.TrackRepo)
		wantCreated bool
		wantTitle   string
		wantErr     string
	}{
		{
			name: "new track is created",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "Artist",
				Album:  "Album",
			},
			wantCreated: true,
			wantTitle:   "Track",
		},
		{
			name: "duplicate returns existing track not created",
			input: AddTrackInput{
				Title:  "Existing",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				seedTrack(t, repo, userId, "Existing", "Artist", "Album")
			},
			wantCreated: false,
			wantTitle:   "Existing",
		},
		{
			name: "empty title returns validation error",
			input: AddTrackInput{
				Title:  "",
				Artist: "Artist",
				Album:  "Album",
			},
			wantErr: "track title required",
		},
		{
			name: "empty artist returns validation error",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "",
				Album:  "Album",
			},
			wantErr: "track artist required",
		},
		{
			name: "repo error propagates",
			input: AddTrackInput{
				Title:  "Track",
				Artist: "Artist",
				Album:  "Album",
			},
			setup: func(repo *catalogtest.TrackRepo) {
				repo.ErrOnAdd = errRepo
			},
			wantErr: "db connection lost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			if tt.setup != nil {
				tt.setup(repo)
			}
			svc := NewAddTrackService(repo)

			out, err := svc.Execute(ctx, userId, tt.input)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out.Created != tt.wantCreated {
				t.Errorf("Created = %v, want %v", out.Created, tt.wantCreated)
			}
			if out.Track == nil {
				t.Fatal("expected non-nil Track in output")
			}
			if out.Track.Title != tt.wantTitle {
				t.Errorf("Track.Title = %q, want %q", out.Track.Title, tt.wantTitle)
			}
		})
	}
}
