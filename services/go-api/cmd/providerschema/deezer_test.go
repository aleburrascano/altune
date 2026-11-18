package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

type deezerCall struct {
	method string
	uri    string
	auth   string
	ctype  string
	body   string
}

type deezerFake struct {
	calls  []deezerCall
	logins atomic.Int32
	reply  string
	status int
}

func (f *deezerFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.calls = append(f.calls, deezerCall{r.Method, r.URL.RequestURI(), r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(body)})
	if r.URL.Path == "/login/anonymous" {
		f.logins.Add(1)
		_, _ = w.Write([]byte(`{"jwt":"anon-jwt","refresh_token":"r"}`))
		return
	}
	if f.status != 0 {
		w.WriteHeader(f.status)
	}
	_, _ = w.Write([]byte(f.reply))
}

func newDeezerFake(t *testing.T, reply string) (*deezerFake, liveSource) {
	t.Helper()
	fake := &deezerFake{reply: reply}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	urls := deezerURLs{api: srv.URL, auth: srv.URL + "/login/anonymous?jo=p&rto=c&i=c", pipe: srv.URL + "/api"}
	return fake, newDeezerSource(srv.Client(), urls, newIntervalGate(0))
}

func TestDeezerSendsTheAdaptersRESTRequests(t *testing.T) {
	cases := map[string]struct{ entity, id, uri string }{
		"structured track search": {"search_track", `Nia "X" Archives|Sober Feels`, "/search/track?q=artist%3A%22Nia+X+Archives%22+track%3A%22Sober+Feels%22&limit=15&order=RANKING"},
		"structured album search": {"search_album", "Nia Archives|Emotional Junglist", "/search/album?q=artist%3A%22Nia+Archives%22+album%3A%22Emotional+Junglist%22&limit=15&order=RANKING"},
		"free-text search":        {"search_album", "Nia Archives", "/search/album?q=Nia+Archives&limit=15&order=RANKING"},
		"artist search":           {"search_artist", "Nia Archives", "/search/artist?q=Nia+Archives&limit=15&order=RANKING"},
		"track chart":             {"chart_tracks", "100", "/chart/0/tracks?limit=100"},
		"album chart":             {"chart_albums", "100", "/chart/0/albums?limit=100"},
		"artist chart":            {"chart_artists", "50", "/chart/0/artists?limit=50"},
		"album tracks page":       {"album_tracks", "211450032", "/album/211450032/tracks?limit=100&index=0"},
		"artist albums page":      {"artist_albums", "96293712", "/artist/96293712/albums?limit=100&index=0"},
		"artist top":              {"artist_top", "96293712", "/artist/96293712/top?limit=10"},
		"track":                   {"track", "4144817442", "/track/4144817442"},
		"album with escaped id":   {"album", "1/2", "/album/1%2F2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { checkDeezerGet(t, c.entity, c.id, c.uri) })
	}
}

func checkDeezerGet(t *testing.T, entity, id, uri string) {
	t.Helper()
	fake, src := newDeezerFake(t, `{"data":[]}`)
	raw, err := src.Resource(context.Background(), entity, id)
	if err != nil || string(raw) != `{"data":[]}` {
		t.Fatalf("got %s, %v", raw, err)
	}
	if want := []deezerCall{{method: http.MethodGet, uri: uri}}; !slices.Equal(fake.calls, want) {
		t.Fatalf("calls %+v, want %+v", fake.calls, want)
	}
}

func TestDeezerFailsOnAnAPIErrorEnvelope(t *testing.T) {
	_, src := newDeezerFake(t, `{"error":{"type":"DataException","message":"no data","code":800}}`)
	_, err := src.Resource(context.Background(), "track", "1")
	if err == nil || !strings.Contains(err.Error(), "800") {
		t.Fatalf("got %v", err)
	}
}

func TestDeezerFailsOnAnErrorStatus(t *testing.T) {
	fake, src := newDeezerFake(t, `{}`)
	fake.status = http.StatusServiceUnavailable
	for _, entity := range []string{"track", "lyrics"} {
		if _, err := src.Resource(context.Background(), entity, "1"); err == nil || !strings.Contains(err.Error(), "status 503") {
			t.Fatalf("%s: got %v", entity, err)
		}
	}
}

func TestDeezerLyricsPostsTheGraphQLQueryWithAnAnonymousToken(t *testing.T) {
	fake, src := newDeezerFake(t, `{"data":{"track":{"id":"7","lyrics":null}}}`)
	for _, id := range []string{"7", "8"} {
		if _, err := src.Resource(context.Background(), "lyrics", id); err != nil {
			t.Fatal(err)
		}
	}
	if got := fake.logins.Load(); got != 1 {
		t.Fatalf("logged in %d times, want once", got)
	}
	if login, want := fake.calls[0], (deezerCall{method: http.MethodGet, uri: "/login/anonymous?jo=p&rto=c&i=c"}); login != want {
		t.Fatalf("login %+v, want %+v", login, want)
	}
	checkLyricsPost(t, fake.calls[1], "7")
}

type lyricsPayload struct {
	OperationName string `json:"operationName"`
	Query         string `json:"query"`
	Variables     struct {
		TrackID string `json:"trackId"`
	} `json:"variables"`
}

func checkLyricsPost(t *testing.T, post deezerCall, trackID string) {
	t.Helper()
	var payload lyricsPayload
	if err := json.Unmarshal([]byte(post.body), &payload); err != nil {
		t.Fatal(err)
	}
	want := lyricsPayload{OperationName: "SynchronizedLyrics", Query: deezerLyricsQuery}
	want.Variables.TrackID = trackID
	if payload != want {
		t.Fatalf("payload %+v, want %+v", payload, want)
	}
	post.body = ""
	if wantCall := (deezerCall{http.MethodPost, "/api", "Bearer anon-jwt", "application/json", ""}); post != wantCall {
		t.Fatalf("post %+v, want %+v", post, wantCall)
	}
}

func TestDeezerAnonymousAuthReturnsTheLoginBody(t *testing.T) {
	_, src := newDeezerFake(t, `{}`)
	raw, err := src.Resource(context.Background(), "auth_anonymous", "anonymous")
	if err != nil || !strings.Contains(string(raw), `"jwt"`) {
		t.Fatalf("got %s, %v", raw, err)
	}
}

func TestDeezerLyricsFailsWhenTheLoginHasNoToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jwt":" "}`))
	}))
	t.Cleanup(srv.Close)
	src := newDeezerSource(srv.Client(), deezerURLs{api: srv.URL, auth: srv.URL, pipe: srv.URL}, newIntervalGate(0))
	if _, err := src.Resource(context.Background(), "lyrics", "7"); err == nil || !strings.Contains(err.Error(), "no jwt") {
		t.Fatalf("got %v", err)
	}
}
