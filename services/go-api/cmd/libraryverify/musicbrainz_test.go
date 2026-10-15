package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

func newTestMusicBrainz(handler http.HandlerFunc) (*musicBrainz, func()) {
	server := httptest.NewServer(handler)
	m := newMusicBrainz(server.Client(), server.URL, 1000)
	m.backoff = time.Millisecond
	return m, server.Close
}

func TestRecordingsByISRC_ReturnsTheRecordingsOfTheCanonicalISRC(t *testing.T) {
	var gotPath, gotAgent string
	m, closeServer := newTestMusicBrainz(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAgent = r.URL.Path, r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(`{"isrc":"USAAA0000001","recordings":[{"id":"rec-a"},{"id":"rec-b"}]}`))
	})
	defer closeServer()

	got, err := m.RecordingsByISRC(context.Background(), "US-AAA-00-00001")
	if err != nil {
		t.Fatalf("RecordingsByISRC: %v", err)
	}
	if !slices.Equal(got, []string{"rec-a", "rec-b"}) {
		t.Errorf("recordings = %v, want [rec-a rec-b]", got)
	}
	if gotPath != "/ws/2/isrc/USAAA0000001" {
		t.Errorf("path = %q, want the canonical ISRC", gotPath)
	}
	if gotAgent != musicBrainzUserAgent {
		t.Errorf("user agent = %q, want %q", gotAgent, musicBrainzUserAgent)
	}
}

func TestRecordingsByISRC_UnknownISRCHasNoRecordings(t *testing.T) {
	m, closeServer := newTestMusicBrainz(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	defer closeServer()

	got, err := m.RecordingsByISRC(context.Background(), "USAAA0000001")

	if err != nil || len(got) != 0 {
		t.Errorf("got %v, %v; want no recordings and no error for a 404", got, err)
	}
}

func TestCanonicalRecording_FollowsAMergedIDToTheSurvivor(t *testing.T) {
	m, closeServer := newTestMusicBrainz(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"rec-survivor","title":"Song"}`))
	})
	defer closeServer()

	got, err := m.CanonicalRecording(context.Background(), "rec-old")

	if err != nil || got != "rec-survivor" {
		t.Errorf("got %q, %v; want rec-survivor", got, err)
	}
}

func TestGetJSON_RetriesAThrottleThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	m, closeServer := newTestMusicBrainz(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"id":"rec-a"}`))
	})
	defer closeServer()

	got, err := m.CanonicalRecording(context.Background(), "rec-a")

	if err != nil || got != "rec-a" || calls.Load() != 2 {
		t.Errorf("got %q, %v after %d calls; want rec-a after 2", got, err, calls.Load())
	}
}

func TestGetJSON_GivesUpWhenStillThrottled(t *testing.T) {
	var calls atomic.Int32
	m, closeServer := newTestMusicBrainz(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	})
	defer closeServer()

	_, err := m.CanonicalRecording(context.Background(), "rec-a")

	if !errors.Is(err, errMusicBrainzThrottled) || calls.Load() != musicBrainzAttempts {
		t.Errorf("err = %v after %d calls; want errMusicBrainzThrottled after %d", err, calls.Load(), musicBrainzAttempts)
	}
}

func TestGetJSON_ServerErrorAndGarbageBodyAreErrorsNotAbsence(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"500":     func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"garbage": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
	} {
		t.Run(name, func(t *testing.T) {
			m, closeServer := newTestMusicBrainz(handler)
			defer closeServer()

			if _, err := m.RecordingsByISRC(context.Background(), "USAAA0000001"); err == nil {
				t.Error("want an error, got nil")
			}
		})
	}
}
