package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const trackJSON = `{"id":7,"title":"Valentina","isrc":"USX","duration":236,"release_date":"2019-03-29",
"artist":{"name":"Daniel Caesar"},"contributors":[{"name":"Daniel Caesar"}],
"album":{"title":"Freudian","md5_image":"abc","cover_medium":"http://img/abc.jpg"}}`

func clientFor(t *testing.T, handler http.HandlerFunc) *deezerClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := newDeezerClient(server.Client(), server.URL, 1000)
	client.backoff = time.Millisecond
	return client
}

func TestTrackByISRC_MapsTheCatalogRecord(t *testing.T) {
	var path string
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(trackJSON))
	})
	got, err := client.TrackByISRC(context.Background(), "USX")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/2.0/track/isrc:USX" {
		t.Fatalf("path %s", path)
	}
	want := catalogTrack{
		ID: 7, Title: "Valentina", Artist: "Daniel Caesar", Contributors: []string{"Daniel Caesar"},
		Album: "Freudian", AlbumImageMD5: "abc", CoverURL: "http://img/abc.jpg", DurationSeconds: 236, ReleaseYear: 2019, ISRC: "USX",
	}
	if got.ID != want.ID || got.Title != want.Title || got.Album != want.Album || got.AlbumImageMD5 != want.AlbumImageMD5 ||
		got.CoverURL != want.CoverURL || got.DurationSeconds != want.DurationSeconds || got.ReleaseYear != want.ReleaseYear ||
		got.ISRC != want.ISRC || len(got.Contributors) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestTrackByISRC_NoDataErrorMeansUnknown(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"error":{"type":"DataException","message":"no data","code":800}}`))
	})
	if _, err := client.TrackByISRC(context.Background(), "NOPE"); !errors.Is(err, errCatalogUnknown) {
		t.Fatalf("got %v", err)
	}
}

func TestTrackByISRC_RetriesQuotaErrorThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			_, _ = w.Write([]byte(`{"error":{"type":"Exception","message":"Quota limit exceeded","code":4}}`))
			return
		}
		_, _ = w.Write([]byte(trackJSON))
	})
	got, err := client.TrackByISRC(context.Background(), "USX")
	if err != nil || got.Title != "Valentina" || calls.Load() != 3 {
		t.Fatalf("err %v title %q calls %d", err, got.Title, calls.Load())
	}
}

func TestTrackByISRC_GivesUpWhenQuotaNeverClears(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"error":{"code":4}}`))
	})
	if _, err := client.TrackByISRC(context.Background(), "USX"); err == nil || errors.Is(err, errCatalogUnknown) {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != deezerMaxAttempts {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestTrackByISRC_HTTPFailureIsAnError(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	if _, err := client.TrackByISRC(context.Background(), "USX"); err == nil || errors.Is(err, errCatalogUnknown) {
		t.Fatalf("got %v", err)
	}
}

func TestSearch_ReturnsCandidates(t *testing.T) {
	var query string
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`{"data":[` + trackJSON + `]}`))
	})
	got, err := client.Search(context.Background(), "Daniel Caesar", "Valentina")
	if err != nil || len(got) != 1 || got[0].ID != 7 {
		t.Fatalf("err %v got %+v", err, got)
	}
	if query != "Daniel Caesar Valentina" {
		t.Fatalf("query %q", query)
	}
}

func TestClient_RateLimitSpacesRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(trackJSON)) }))
	defer server.Close()
	client := newDeezerClient(server.Client(), server.URL, 20)
	start := time.Now()
	for range 4 {
		if _, err := client.TrackByID(context.Background(), 7); err != nil {
			t.Fatal(err)
		}
	}
	if elapsed := time.Since(start); elapsed < 120*time.Millisecond {
		t.Fatalf("4 requests at 20/s took only %s", elapsed)
	}
}

func TestTrackByISRC_ForbiddenIsABlockAndIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("<html>Access Denied</html>"))
	})
	if _, err := client.TrackByISRC(context.Background(), "USX"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("got %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls %d", calls.Load())
	}
}

func TestTrackByISRC_NonJSONResponseIsABlock(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>Access Denied</html>"))
	})
	if _, err := client.TrackByISRC(context.Background(), "USX"); !errors.Is(err, errCatalogBlocked) {
		t.Fatalf("got %v", err)
	}
}

func TestTrackByISRC_TooManyRequestsBacksOffThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(trackJSON))
	})
	got, err := client.TrackByISRC(context.Background(), "USX")
	if err != nil || got.Title != "Valentina" || calls.Load() != 3 {
		t.Fatalf("err %v title %q calls %d", err, got.Title, calls.Load())
	}
}

func TestTrackByISRC_RepeatedThrottlingBecomesABlock(t *testing.T) {
	for name, respond := range map[string]http.HandlerFunc{
		"http 429":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		"quota code": func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"error":{"code":4}}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				respond(w, r)
			})
			if _, err := client.TrackByISRC(context.Background(), "USX"); !errors.Is(err, errCatalogBlocked) {
				t.Fatalf("got %v", err)
			}
			if calls.Load() != deezerMaxAttempts {
				t.Fatalf("calls %d", calls.Load())
			}
		})
	}
}

func TestDelayBefore_DoublesEachAttemptUpToTheCeiling(t *testing.T) {
	client := newDeezerClient(http.DefaultClient, "", 1)
	client.backoff, client.ceiling = time.Second, 5*time.Second
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 5 * time.Second, 5 * time.Second}
	for i, expected := range want {
		if got := client.delayBefore(i + 1); got != expected {
			t.Fatalf("attempt %d: got %s want %s", i+1, got, expected)
		}
	}
}

func TestTrackByISRC_MapsTheAlbumIdAndTrackPosition(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"title":"Valentina","track_position":4,"disk_number":2,"album":{"id":99,"title":"Freudian"}}`))
	})
	got, err := client.TrackByISRC(context.Background(), "USX")
	if err != nil || got.AlbumID != 99 || got.TrackPosition != 4 || got.DiskNumber != 2 {
		t.Fatalf("err=%v got=%+v", err, got)
	}
}

func TestAlbumGenres_ReadsTheGenreNamesAndAsksOncePerAlbum(t *testing.T) {
	var calls atomic.Int32
	var path string
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		path = r.URL.Path
		_, _ = w.Write([]byte(`{"genres":{"data":[{"id":1,"name":"R&B"},{"id":2,"name":"Pop"}]}}`))
	})
	for range 3 {
		got, err := client.AlbumGenres(context.Background(), 99)
		if err != nil || len(got) != 2 || got[0] != "R&B" {
			t.Fatalf("err=%v got=%v", err, got)
		}
	}
	if path != "/2.0/album/99" || calls.Load() != 1 {
		t.Fatalf("path %s calls %d", path, calls.Load())
	}
}

func TestAlbumGenres_AFailedLookupIsNotCached(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"error":{"code":800}}`))
			return
		}
		_, _ = w.Write([]byte(`{"genres":{"data":[{"name":"Pop"}]}}`))
	})
	if _, err := client.AlbumGenres(context.Background(), 5); !errors.Is(err, errCatalogUnknown) {
		t.Fatalf("got %v", err)
	}
	if got, err := client.AlbumGenres(context.Background(), 5); err != nil || len(got) != 1 {
		t.Fatalf("err=%v got=%v", err, got)
	}
}

func TestAlbumRecordType_ReadsTheRecordTypeFromTheCachedAlbum(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"record_type":"single","genres":{"data":[{"name":"Pop"}]}}`))
	})
	recordType, err := client.AlbumRecordType(context.Background(), 5)
	genres, genresErr := client.AlbumGenres(context.Background(), 5)
	if err != nil || genresErr != nil || recordType != "single" || len(genres) != 1 || calls.Load() != 1 {
		t.Fatalf("type=%q err=%v genres=%v genresErr=%v calls=%d", recordType, err, genres, genresErr, calls.Load())
	}
}

func TestTrackByISRC_PrefersTheLargestAlbumCoverForReplacement(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":7,"album":{"cover_medium":"m","cover_big":"b","cover_xl":"xl"}}`))
	})
	got, err := client.TrackByISRC(context.Background(), "USX")
	if err != nil || got.FullCoverURL != "xl" || got.CoverURL != "m" {
		t.Fatalf("err=%v got=%+v", err, got)
	}
}

func TestAlbumGenres_ConcurrentCallsForOneAlbumFetchItOnce(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"genres":{"data":[{"id":1,"name":"R&B"}]}}`))
	})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if got, err := client.AlbumGenres(context.Background(), 99); err != nil || len(got) != 1 {
				t.Errorf("err=%v got=%v", err, got)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("fetched %d times", calls.Load())
	}
}

func TestAlbumGenres_ASharedFailureReachesEveryWaiterAndIsNotCached(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(100 * time.Millisecond)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"genres":{"data":[{"id":1,"name":"Pop"}]}}`))
	})
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 4 {
		wg.Go(func() {
			if _, err := client.AlbumGenres(context.Background(), 99); err != nil {
				failed.Add(1)
			}
		})
	}
	wg.Wait()
	if failed.Load() != 4 {
		t.Fatalf("failed %d", failed.Load())
	}
	if got, err := client.AlbumGenres(context.Background(), 99); err != nil || got[0] != "Pop" {
		t.Fatalf("retry: %v %v", got, err)
	}
}

func TestDeezerClient_AlbumTracksReadsTheMatchingAlbumOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/2.0/search/album":
			_, _ = w.Write([]byte(`{"data":[
				{"id":6164468,"title":"Stay (Unapologetic) - Nonstop Music Tribute to Rihanna & Mikky Ekko","artist":{"name":"NonStop Music"}},
				{"id":6120368,"title":"Unapologetic","artist":{"name":"Rihanna"}}]}`))
		case "/2.0/album/6120368/tracks":
			_, _ = w.Write([]byte(`{"data":[{"id":62376270,"title":"Diamonds","duration":225,"artist":{"name":"Rihanna"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	tracks, err := newDeezerClient(server.Client(), server.URL, 1000).AlbumTracks(context.Background(), "Rihanna", "Unapologetic")
	if err != nil || len(tracks) != 1 || tracks[0].ID != 62376270 || tracks[0].Album != "Unapologetic" || tracks[0].DurationSeconds != 225 {
		t.Fatalf("got %+v, %v", tracks, err)
	}
}
