package musicbrainz_test

import (
	"altune/go-api/internal/search/internal/adapters/providers/musicbrainz"
	"altune/go-api/internal/search/internal/adapters/providers/providertest"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMusicBrainzAdapter_ReleaseGroupTitles_LeaderCancelDoesNotFailWaiters(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"release-group-count": 1, "release-groups": [` +
			mbReleaseGroupJSON("rg-1", "OK Computer", "Radiohead", "mbid-rh") + `]}`))
	}))
	defer server.Close()
	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0", clock.Now)

	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	leaderErr := make(chan error, 1)
	go func() {
		_, err := adapter.ReleaseGroupTitles(leaderCtx, "mbid-rh")
		leaderErr <- err
	}()
	<-started

	type result struct {
		titles []string
		err    error
	}
	waiter := make(chan result, 1)
	go func() {
		titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-rh")
		waiter <- result{titles, err}
	}()
	time.Sleep(50 * time.Millisecond)

	cancelLeader()
	select {
	case err := <-leaderErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("leader err = %v, want context.Canceled promptly", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled caller did not return promptly")
	}

	close(release)
	select {
	case res := <-waiter:
		if res.err != nil || len(res.titles) != 1 || res.titles[0] != "OK Computer" {
			t.Errorf("waiter = %v, %v; want the release groups despite leader cancel", res.titles, res.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiter never got a result")
	}
}

func TestMusicBrainzAdapter_ReleaseGroupTitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"release-group-count": 2,
			"release-groups": [
				` + mbReleaseGroupJSON("rg-1", "OK Computer", "Radiohead", "mbid-rh") + `,
				` + mbReleaseGroupJSON("rg-2", "Kid A", "Radiohead", "mbid-rh") + `
			]}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0", clock.Now)
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-rh")
	if err != nil {
		t.Fatalf("ReleaseGroupTitles: %v", err)
	}
	if len(titles) != 2 || titles[0] != "OK Computer" || titles[1] != "Kid A" {
		t.Errorf("titles = %v, want the release-group titles in order", titles)
	}
}

func TestMusicBrainzAdapter_ReleaseGroupTitles_laterPageFailureIsNotACompleteAnchor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("offset") != "0" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{
			"release-group-count": 150,
			"release-groups": [` + mbReleaseGroupJSON("rg-1", "OK Computer", "Radiohead", "mbid-rh") + `]}`))
	}))
	defer server.Close()

	adapter := musicbrainz.NewMusicBrainzAdapter(providertest.NewClient(server.URL), "altune-test/1.0", clock.Now)
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-rh")
	if err == nil {
		t.Fatalf("ReleaseGroupTitles = %v, nil; want an error so a truncated title set is not taken as the anchor", titles)
	}
	if len(titles) != 0 {
		t.Errorf("titles = %v, want none from a degraded fetch", titles)
	}
}
