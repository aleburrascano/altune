package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// --- SearchStep ---

func TestSearchStep_Execute(t *testing.T) {
	// Arrange
	searcher := &fakeAudioSearcher{
		searchResults: []ports.AudioCandidate{
			{
				Title:      "Artist - Song Title",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=abc",
				Channel:    "Artist - Topic",
				Categories: []string{"Music"},
				ViewCount:  1_000_000,
			},
			{
				Title:      "Artist - Song Title (Lyrics)",
				Duration:   201,
				URL:        "https://youtube.com/watch?v=def",
				Channel:    "LyricsChannel",
				Categories: []string{"Music"},
				ViewCount:  500_000,
			},
		},
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Song Title",
			Artist: "Artist",
		},
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ac.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ac.Candidates))
	}
	if ac.Candidates[0].URL != "https://youtube.com/watch?v=abc" {
		t.Errorf("candidate[0].URL = %q, want %q", ac.Candidates[0].URL, "https://youtube.com/watch?v=abc")
	}
	if ac.Candidates[1].URL != "https://youtube.com/watch?v=def" {
		t.Errorf("candidate[1].URL = %q, want %q", ac.Candidates[1].URL, "https://youtube.com/watch?v=def")
	}
}

func TestSearchStep_Execute_NoCandidates(t *testing.T) {
	// Arrange
	searcher := &fakeAudioSearcher{
		searchResults: []ports.AudioCandidate{},
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Nonexistent Song",
			Artist: "Nobody",
		},
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err == nil {
		t.Fatal("expected error for no candidates, got nil")
	}
	if got := err.Error(); got != "no candidates found" {
		t.Errorf("error = %q, want %q", got, "no candidates found")
	}
}

func TestSearchStep_Execute_SearchError_NoCandidates(t *testing.T) {
	// Arrange: searcher returns an error for every query
	searcher := &fakeAudioSearcher{
		searchResults: nil,
		searchErr:     fmt.Errorf("network timeout"),
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Some Song",
			Artist: "Some Artist",
		},
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert: all queries failed, so 0 candidates => error
	if err == nil {
		t.Fatal("expected error when all searches fail, got nil")
	}
	if got := err.Error(); got != "no candidates found" {
		t.Errorf("error = %q, want %q", got, "no candidates found")
	}
}

func TestSearchStep_Execute_DeduplicatesByURL(t *testing.T) {
	// Arrange: same URL returned across different query iterations
	searcher := &fakeAudioSearcher{
		searchResults: []ports.AudioCandidate{
			{
				Title:      "Artist - Song",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=same",
				Channel:    "ArtistVEVO",
				Categories: []string{"Music"},
			},
		},
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Song",
			Artist: "Artist",
			Album:  "Album", // generates multiple queries
			ISRC:   "US1234567890",
		},
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert: multiple queries see the same URL, but dedup keeps only 1
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ac.Candidates) != 1 {
		t.Errorf("expected 1 deduplicated candidate, got %d", len(ac.Candidates))
	}
}

func TestSearchStep_Name(t *testing.T) {
	step := NewSearchStep(nil)
	if got := step.Name(); got != "search" {
		t.Errorf("Name() = %q, want %q", got, "search")
	}
}

// --- SelectStep ---

func TestSelectStep_Execute(t *testing.T) {
	// Arrange: provide candidates that will pass matching gates.
	// Using a Topic channel candidate to ensure it passes the identity threshold.
	step := NewSelectStep()
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:    "Blinding Lights",
			Artist:   "The Weeknd",
			Duration: 200,
		},
		Candidates: []ports.AudioCandidate{
			{
				Title:      "Blinding Lights",
				Channel:    "The Weeknd - Topic",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=topic1",
				Categories: []string{"Music"},
				ViewCount:  10_000_000,
			},
		},
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ac.Selected == nil {
		t.Fatal("expected ac.Selected to be populated, got nil")
	}
	if ac.Selected.URL != "https://youtube.com/watch?v=topic1" {
		t.Errorf("Selected.URL = %q, want %q", ac.Selected.URL, "https://youtube.com/watch?v=topic1")
	}
}

func TestSelectStep_Execute_NoCandidates(t *testing.T) {
	// Arrange: empty candidates list
	step := NewSelectStep()
	ac := &AcquisitionContext{
		Track:      TrackRef{Title: "Song", Artist: "Artist", Duration: 200},
		Candidates: []ports.AudioCandidate{},
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err == nil {
		t.Fatal("expected error for no candidates passing gates, got nil")
	}
	if got := err.Error(); got != "no candidates passed matching gates" {
		t.Errorf("error = %q, want %q", got, "no candidates passed matching gates")
	}
}

func TestSelectStep_Execute_AllCandidatesBelowThreshold(t *testing.T) {
	// Arrange: candidates whose titles don't match the track at all
	step := NewSelectStep()
	ac := &AcquisitionContext{
		Track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []ports.AudioCandidate{
			{
				Title:      "Cooking Tutorial Episode 47",
				Channel:    "CookingChannel",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=cook1",
				Categories: []string{"Howto & Style"},
				ViewCount:  50_000,
			},
		},
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err == nil {
		t.Fatal("expected error when all candidates are below identity threshold, got nil")
	}
}

func TestSelectStep_Name(t *testing.T) {
	step := NewSelectStep()
	if got := step.Name(); got != "select" {
		t.Errorf("Name() = %q, want %q", got, "select")
	}
}

// --- StoreStep ---

func TestStoreStep_Execute(t *testing.T) {
	// Arrange
	store := newFakeAudioStore()
	step := NewStoreStep(store)
	ac := &AcquisitionContext{
		Track: TrackRef{
			UserID: "user-123",
			Title:  "Song Title",
			Artist: "Artist Name",
			Album:  "Album Name",
		},
		TempPath: "/tmp/altune-test/song.mp3",
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ac.AudioRef == "" {
		t.Fatal("expected ac.AudioRef to be set, got empty string")
	}
	// Verify the audio ref follows the expected pattern: userID/artist/album/title.mp3
	wantRef := "user-123/Artist Name/Album Name/Song Title.mp3"
	if ac.AudioRef != wantRef {
		t.Errorf("AudioRef = %q, want %q", ac.AudioRef, wantRef)
	}
	// Verify the fake store actually stored it
	if !store.stored[ac.AudioRef] {
		t.Error("expected audio ref to be stored in fake store")
	}
}

// TestStoreStep_Execute_RejectsUndecodable is the final safety net: even a file
// that reached the store step is decode-validated first, so corruption introduced
// after download (e.g. a format-mismatched tagger) never reaches the library.
func TestStoreStep_Execute_RejectsUndecodable(t *testing.T) {
	store := newFakeAudioStore()
	prober := &queueProber{decodeErrs: []error{errors.New("audio stream failed to decode")}}
	step := NewStoreStep(store, WithStoreProber(prober))
	ac := &AcquisitionContext{
		Track:    TrackRef{UserID: "u", Title: "T", Artist: "A"},
		TempPath: "/tmp/altune-test/song.mp3",
	}

	if err := step.Execute(context.Background(), ac); err == nil {
		t.Fatal("expected store to reject an undecodable final file")
	}
	if len(store.stored) != 0 {
		t.Errorf("nothing should be stored when validation fails, got %d", len(store.stored))
	}
}

func TestStoreStep_Execute_NoTempPath(t *testing.T) {
	// Arrange
	store := newFakeAudioStore()
	step := NewStoreStep(store)
	ac := &AcquisitionContext{
		TempPath: "", // no temp file
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err == nil {
		t.Fatal("expected error for missing temp path, got nil")
	}
	if got := err.Error(); got != "no temp file to store" {
		t.Errorf("error = %q, want %q", got, "no temp file to store")
	}
}

func TestStoreStep_Execute_StoreError(t *testing.T) {
	// Arrange
	store := newFakeAudioStore()
	store.err = fmt.Errorf("storage unavailable")
	step := NewStoreStep(store)
	ac := &AcquisitionContext{
		Track: TrackRef{
			UserID: "user-123",
			Title:  "Song",
			Artist: "Artist",
		},
		TempPath: "/tmp/altune-test/song.mp3",
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err == nil {
		t.Fatal("expected error when store fails, got nil")
	}
}

func TestStoreStep_Rollback_DeletesStoredAudio(t *testing.T) {
	// Arrange
	store := newFakeAudioStore()
	store.stored["user-123/Artist/Album/Song.mp3"] = true
	step := NewStoreStep(store)
	ac := &AcquisitionContext{
		AudioRef: "user-123/Artist/Album/Song.mp3",
	}

	// Act
	err := step.Rollback(context.Background(), ac)

	// Assert
	if err != nil {
		t.Fatalf("expected no error on rollback, got %v", err)
	}
	if store.stored["user-123/Artist/Album/Song.mp3"] {
		t.Error("expected audio ref to be deleted from store after rollback")
	}
}

func TestStoreStep_Name(t *testing.T) {
	step := NewStoreStep(nil)
	if got := step.Name(); got != "store" {
		t.Errorf("Name() = %q, want %q", got, "store")
	}
}

// --- UpdateTrackStep ---

func TestUpdateTrackStep_Execute(t *testing.T) {
	// Arrange
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{
		AudioRef: "user/artist/album/song.mp3",
	}

	// Act
	execErr := step.Execute(context.Background(), ac)

	// Assert
	if execErr != nil {
		t.Fatalf("expected no error, got %v", execErr)
	}

	// Verify track was marked ready
	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("track status = %v, want %v", updated.AcquisitionStatus, domain.AcquisitionReady)
	}
	if updated.AudioRef == nil || *updated.AudioRef != "user/artist/album/song.mp3" {
		t.Errorf("track AudioRef = %v, want %q", updated.AudioRef, "user/artist/album/song.mp3")
	}
}

func TestUpdateTrackStep_Execute_TrackNotFound(t *testing.T) {
	// Arrange: empty repo, track doesn't exist
	repo := newFakeTrackRepository()
	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	step := NewUpdateTrackStep(repo, userId, trackId)
	ac := &AcquisitionContext{
		AudioRef: "some/audio/ref.mp3",
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err == nil {
		t.Fatal("expected error when track not found, got nil")
	}
	if got := err.Error(); got != "track not found for update" {
		t.Errorf("error = %q, want %q", got, "track not found for update")
	}
}

func TestUpdateTrackStep_Execute_EmptyAudioRef(t *testing.T) {
	// Arrange: track exists but audioRef is empty -> MarkReady should fail
	userId := shared.NewUserId(uuid.New())
	track, _ := domain.NewTrack(userId, "Song", "Artist", "Album")

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{
		AudioRef: "", // empty
	}

	// Act
	err := step.Execute(context.Background(), ac)

	// Assert
	if err == nil {
		t.Fatal("expected error when audioRef is empty, got nil")
	}
}

func TestUpdateTrackStep_Rollback_RevertsToPending(t *testing.T) {
	// Arrange: track is in ready state
	userId := shared.NewUserId(uuid.New())
	track, _ := domain.NewTrack(userId, "Song", "Artist", "Album")
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{}

	// Act
	err := step.Rollback(context.Background(), ac)

	// Assert
	if err != nil {
		t.Fatalf("expected no error on rollback, got %v", err)
	}
	reverted := repo.tracks[track.ID.String()+":"+userId.String()]
	if reverted.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("track status after rollback = %v, want %v", reverted.AcquisitionStatus, domain.AcquisitionPending)
	}
}

func TestUpdateTrackStep_Name(t *testing.T) {
	step := NewUpdateTrackStep(nil, shared.UserId{}, domain.TrackId{})
	if got := step.Name(); got != "update_track" {
		t.Errorf("Name() = %q, want %q", got, "update_track")
	}
}

// --- buildAudioRef + sanitizePathComponent ---

func TestBuildAudioRef(t *testing.T) {
	tests := []struct {
		name  string
		track TrackRef
		want  string
	}{
		{
			name: "normal track",
			track: TrackRef{
				UserID: "uid",
				Artist: "The Weeknd",
				Album:  "After Hours",
				Title:  "Blinding Lights",
			},
			want: "uid/The Weeknd/After Hours/Blinding Lights.mp3",
		},
		{
			name: "empty album defaults to Unknown Album",
			track: TrackRef{
				UserID: "uid",
				Artist: "Artist",
				Album:  "",
				Title:  "Song",
			},
			want: "uid/Artist/Unknown Album/Song.mp3",
		},
		{
			name: "forbidden chars stripped",
			track: TrackRef{
				UserID: "uid",
				Artist: "AC/DC",
				Album:  `The "Best" Album`,
				Title:  "Song: Title?",
			},
			want: "uid/ACDC/The Best Album/Song Title.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildAudioRef(tt.track, "/tmp/acquire/downloaded.mp3")
			if got != tt.want {
				t.Errorf("buildAudioRef() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizePathComponent(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: "Unknown"},
		{name: "all forbidden chars", input: `<>:"/\|?*;`, want: "Unknown"},
		{name: "normal string", input: "Hello World", want: "Hello World"},
		{name: "mixed content", input: `Song: "Title"`, want: "Song Title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizePathComponent(tt.input)
			if got != tt.want {
				t.Errorf("sanitizePathComponent(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
