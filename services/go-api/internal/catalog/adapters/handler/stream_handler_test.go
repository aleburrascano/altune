package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestHandleStreamAudio(t *testing.T) {
	tests := []struct {
		name            string
		setup           func(*fakeTrackRepo, *fakeAudioStore) string
		wantStatus      int
		wantContentType string
		wantBodyLen     int
	}{
		{
			name: "ready track streams audio bytes",
			setup: func(repo *fakeTrackRepo, store *fakeAudioStore) string {
				track := makeReadyTrack(testUserId, "Song", "Artist", "Album", "audio/123.opus")
				repo.seed(track)
				store.seed("audio/123.opus", []byte("fake-audio-data"))
				return track.ID.UUID().String()
			},
			wantStatus:      http.StatusOK,
			wantContentType: "audio/mpeg",
			wantBodyLen:     len("fake-audio-data"),
		},
		{
			name: "invalid track ID returns 400",
			setup: func(repo *fakeTrackRepo, store *fakeAudioStore) string {
				return "not-a-uuid"
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "track not found returns 404",
			setup: func(repo *fakeTrackRepo, store *fakeAudioStore) string {
				return uuid.New().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "pending track (not streamable) returns 404",
			setup: func(repo *fakeTrackRepo, store *fakeAudioStore) string {
				track := makeTrack(testUserId, "Pending", "Artist", "Album")
				repo.seed(track)
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "ready track with missing audio file returns 404 and reconciles",
			setup: func(repo *fakeTrackRepo, store *fakeAudioStore) string {
				track := makeReadyTrack(testUserId, "Gone", "Artist", "Album", "audio/gone.opus")
				repo.seed(track)
				// Deliberately NOT seeding audio store, so Stream will fail with EOF
				return track.ID.UUID().String()
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			repo := newFakeTrackRepo()
			store := newFakeAudioStore()
			trackId := tt.setup(repo, store)
			_, router := buildStreamHandler(repo, store, nil)

			// Act
			rec := serve(t, router, http.MethodGet, "/tracks/"+trackId+"/stream", nil)

			// Assert
			assertStatus(t, rec, tt.wantStatus)

			if tt.wantContentType != "" {
				ct := rec.Header().Get("Content-Type")
				if ct != tt.wantContentType {
					t.Errorf("Content-Type = %q, want %q", ct, tt.wantContentType)
				}
			}
			if tt.wantBodyLen > 0 {
				if rec.Body.Len() != tt.wantBodyLen {
					t.Errorf("body length = %d, want %d", rec.Body.Len(), tt.wantBodyLen)
				}
			}
		})
	}
}

func TestHandleStreamAudio_MissingAudio_SchedulesReacquisition(t *testing.T) {
	repo := newFakeTrackRepo()
	store := newFakeAudioStore()
	sched := &fakeScheduler{}
	track := makeReadyTrack(testUserId, "Gone", "Artist", "Album", "audio/gone.opus")
	repo.seed(track)

	_, router := buildStreamHandler(repo, store, sched)
	rec := serve(t, router, http.MethodGet, "/tracks/"+track.ID.UUID().String()+"/stream", nil)

	assertStatus(t, rec, http.StatusNotFound)

	if len(sched.scheduled) != 1 {
		t.Fatalf("expected 1 scheduled reacquisition, got %d", len(sched.scheduled))
	}
	if sched.scheduled[0] != track.ID {
		t.Errorf("scheduled track = %v, want %v", sched.scheduled[0], track.ID)
	}
}

func TestHandleStreamAudio_NoAuth(t *testing.T) {
	// Arrange
	repo := newFakeTrackRepo()
	store := newFakeAudioStore()
	_, router := buildStreamHandler(repo, store, nil)

	// Act
	rec := serveNoAuth(t, router, http.MethodGet, "/tracks/"+uuid.New().String()+"/stream", nil)

	// Assert
	assertStatus(t, rec, http.StatusUnauthorized)
}
