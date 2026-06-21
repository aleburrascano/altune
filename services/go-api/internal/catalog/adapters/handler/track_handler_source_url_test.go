package handler

import (
	"net/http"
	"testing"
)

// The create endpoint forwards the discovered source URL to the acquisition
// scheduler so acquisition can download that exact track (acquire-soundcloud).
func TestHandleCreateTrack_ForwardsSourceURLToScheduler(t *testing.T) {
	repo := newFakeTrackRepo()
	sched := &fakeScheduler{}
	_, router := buildTrackHandler(repo, sched)

	scURL := "https://soundcloud.com/liltecca/fell-in-love"
	body := CreateTrackRequest{
		Title:     "Fell In Love",
		Artist:    "Lil Tecca",
		SourceURL: strPtr(scURL),
	}

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))
	assertStatus(t, rec, http.StatusCreated)

	if len(sched.sourceURLs) != 1 || sched.sourceURLs[0] != scURL {
		t.Fatalf("scheduler should receive the source URL %q, got %v", scURL, sched.sourceURLs)
	}
}

// A save with no source URL forwards an empty string (acquisition falls back to
// search).
func TestHandleCreateTrack_NoSourceURL_ForwardsEmpty(t *testing.T) {
	repo := newFakeTrackRepo()
	sched := &fakeScheduler{}
	_, router := buildTrackHandler(repo, sched)

	body := CreateTrackRequest{Title: "Some Track", Artist: "Some Artist"}

	rec := serve(t, router, http.MethodPost, "/tracks", jsonBody(t, body))
	assertStatus(t, rec, http.StatusCreated)

	if len(sched.sourceURLs) != 1 || sched.sourceURLs[0] != "" {
		t.Fatalf("scheduler should receive an empty source URL, got %v", sched.sourceURLs)
	}
}
