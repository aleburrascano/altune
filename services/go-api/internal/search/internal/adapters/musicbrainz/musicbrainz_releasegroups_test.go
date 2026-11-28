package musicbrainz_test

import (
	"altune/go-api/internal/search/internal/adapters/musicbrainz"
	"altune/go-api/internal/search/internal/contracttest"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"testing/synctest"
)

type handlerTransport struct{ h http.Handler }

func (tr handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	tr.h.ServeHTTP(rec, req)
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	return rec.Result(), nil
}

func TestMusicBrainzAdapter_ReleaseGroupTitles_LeaderCancelDoesNotFailWaiters(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var requests atomic.Int32
		client := &http.Client{Transport: handlerTransport{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"release-group-count": 1, "release-groups": [` +
				mbReleaseGroupJSON("rg-1", "OK Computer", "Radiohead", "mbid-rh") + `]}`))
		})}}
		adapter := musicbrainz.NewMusicBrainzAdapter(client, "altune-test/1.0", clock.Now)

		leaderCtx, cancelLeader := context.WithCancel(context.Background())
		leaderErr := make(chan error, 1)
		go func() {
			_, err := adapter.ReleaseGroupTitles(leaderCtx, "mbid-rh")
			leaderErr <- err
		}()
		synctest.Wait()

		type result struct {
			titles []string
			err    error
		}
		waiter := make(chan result, 1)
		go func() {
			titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-rh")
			waiter <- result{titles, err}
		}()
		synctest.Wait()

		cancelLeader()
		if err := <-leaderErr; !errors.Is(err, context.Canceled) {
			t.Fatalf("leader err = %v, want context.Canceled promptly", err)
		}

		close(release)
		res := <-waiter
		if res.err != nil || len(res.titles) != 1 || res.titles[0] != "OK Computer" {
			t.Errorf("waiter = %v, %v; want the release groups despite leader cancel", res.titles, res.err)
		}
		if got := requests.Load(); got != 1 {
			t.Errorf("upstream requests = %d, want 1: the waiter must share the leader's fetch", got)
		}
	})
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

	adapter := musicbrainz.NewMusicBrainzAdapter(contracttest.NewClient(server.URL), "altune-test/1.0", clock.Now)
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

	adapter := musicbrainz.NewMusicBrainzAdapter(contracttest.NewClient(server.URL), "altune-test/1.0", clock.Now)
	titles, err := adapter.ReleaseGroupTitles(context.Background(), "mbid-rh")
	if err == nil {
		t.Fatalf("ReleaseGroupTitles = %v, nil; want an error so a truncated title set is not taken as the anchor", titles)
	}
	if len(titles) != 0 {
		t.Errorf("titles = %v, want none from a degraded fetch", titles)
	}
}
