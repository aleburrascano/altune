package main

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLiveSourceHasAnEmptyReferenceAndFetchesByEntity(t *testing.T) {
	src := liveSource{"track": func(_ context.Context, id string) (json.RawMessage, error) {
		return json.RawMessage(`{"id":"` + id + `"}`), nil
	}}
	ref, err := src.Reference(context.Background(), "track")
	if err != nil || len(ref) != 0 {
		t.Fatalf("got %v, %v", ref, err)
	}
	raw, err := src.Resource(context.Background(), "track", "7")
	if err != nil || string(raw) != `{"id":"7"}` {
		t.Fatalf("got %s, %v", raw, err)
	}
	if _, err := src.Resource(context.Background(), "album", "7"); err == nil {
		t.Fatal("want an error for an entity with no fetcher")
	}
}

func TestFetchSendsTheMethodBodyAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_, _ = w.Write([]byte(r.Method + " " + r.Header.Get("X-Key") + " " + string(body)))
	}))
	t.Cleanup(srv.Close)
	got, err := fetch(context.Background(), srv.Client(), request{method: http.MethodPost, url: srv.URL, body: []byte("q"), header: map[string]string{"X-Key": "k"}})
	if err != nil || string(got) != "POST k q" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFetchFailsOnAnErrorStatusWithoutTheQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	_, err := fetch(context.Background(), srv.Client(), request{method: http.MethodGet, url: srv.URL + "/p?api_key=secret"})
	if err == nil || !strings.Contains(err.Error(), "status 403") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

func TestFetchKeepsTheQueryOutOfTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.Close()
	_, err := fetch(context.Background(), srv.Client(), request{method: http.MethodGet, url: srv.URL + "/p?api_key=secret"})
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v", err)
	}
}

func TestFetchReportsTheStatusAsAStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	_, err := fetch(context.Background(), srv.Client(), request{method: http.MethodGet, url: srv.URL + "/p"})
	var failure statusError
	if !errors.As(err, &failure) || failure.status != http.StatusServiceUnavailable || failure.Error() != "GET "+srv.Listener.Addr().String()+"/p: status 503" {
		t.Fatalf("got %v", err)
	}
}

func TestDecodedTypesListsWhatEachEndpointDecodesInto(t *testing.T) {
	endpoints := map[string]itunesEndpoint{"a": {"aType", ""}, "b": {decodedByKeys, ""}}
	if got := decodedTypes(endpoints); !maps.Equal(got, map[string]string{"a": "aType", "b": ""}) {
		t.Fatalf("got %v", got)
	}
}

func TestFixturePartsSplitsOnThePipeAndFailsOnTheWrongCount(t *testing.T) {
	parts, err := fixtureParts("Nia Archives|Silence Is Loud", "artist", "album")
	if err != nil || !slices.Equal(parts, []string{"Nia Archives", "Silence Is Loud"}) {
		t.Fatalf("got %q, %v", parts, err)
	}
	for _, id := range []string{"Nia Archives", "a|b|c"} {
		if _, err := fixtureParts(id, "artist", "album"); err == nil || !strings.Contains(err.Error(), "artist|album") {
			t.Errorf("%q: got %v", id, err)
		}
	}
}

func TestFixtureNumbersWantsAPositiveNumberInEveryPart(t *testing.T) {
	numbers, err := fixtureNumbers("2696344|2", "artistID", "page")
	if err != nil || !slices.Equal(numbers, []int{2696344, 2}) {
		t.Fatalf("got %v, %v", numbers, err)
	}
	for _, id := range []string{"x|1", "1|0", "1|-3", "1"} {
		if _, err := fixtureNumbers(id, "artistID", "page"); err == nil {
			t.Errorf("%q: want an error", id)
		}
	}
}

func TestIntervalGateSpacesCallsAndHonoursCancel(t *testing.T) {
	gate := newIntervalGate(clock.Now, time.Hour)
	if err := gate.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := gate.wait(ctx); err == nil {
		t.Fatal("want the second call to wait out the gap and hit the deadline")
	}
}

func TestFetchMasksASecretInThePathOfEveryError(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(failing.Close)
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	for _, srv := range []*httptest.Server{failing, closed} {
		_, err := fetch(context.Background(), srv.Client(), request{method: http.MethodGet, url: srv.URL + "/json/SECRET%20KEY/search.php", secret: "SECRET KEY"})
		if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "/json/***/search.php") {
			t.Errorf("got %v", err)
		}
	}
}
