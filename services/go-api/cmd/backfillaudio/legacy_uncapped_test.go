package main

import (
	"altune/go-api/internal/catalog/domain"
	"context"
	"strings"
	"testing"
)

// TestCandidateRefs_StillFindsAPreCapLegacyFile pins that a file stored before
// capSegmentBytes existed, under a segment in the 120-254 byte range (valid
// then, under the 255-byte filesystem limit), is still located: candidateRefs
// must also try the pre-cap legacy path, not only the capped one.
func TestCandidateRefs_StillFindsAPreCapLegacyFile(t *testing.T) {
	c := testCandidate(t)
	c.artist = strings.Repeat("a", 200)

	refs := candidateRefs(c)

	preCapRef := "11111111-1111-1111-1111-111111111111/" + c.artist + "/UNRELEASED/Night Drive.mp3"
	found := false
	for _, ref := range refs {
		if ref == preCapRef {
			found = true
		}
	}
	if !found {
		t.Fatalf("no candidate ref matched the pre-cap legacy path; got %v", refs)
	}
}

// TestReconcile_LocatesAPreCapLegacyFile is the end-to-end regression: a
// track whose stored object lives at the pre-cap legacy path (200-byte
// artist segment) must still be found and marked ready by reconcile.
func TestReconcile_LocatesAPreCapLegacyFile(t *testing.T) {
	c := testCandidate(t)
	c.artist = strings.Repeat("a", 200)
	preCapRef := "11111111-1111-1111-1111-111111111111/" + c.artist + "/UNRELEASED/Night Drive.mp3"

	track, err := domain.NewTrack(c.userID, c.title, c.artist, c.album)
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := &fakeRepo{track: track}
	store := &fakeStore{present: map[string]bool{preCapRef: true}}

	got := reconcile(context.Background(), repo, store, c, true)

	if got.err != nil {
		t.Fatalf("reconcile: %v", got.err)
	}
	if got.ref != preCapRef {
		t.Errorf("ref = %q, want %q", got.ref, preCapRef)
	}
	if repo.updated == nil || !repo.updated.IsStreamable() {
		t.Fatal("track was not persisted as streamable")
	}
}
