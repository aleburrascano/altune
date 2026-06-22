package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// deezerLyricsServer routes the three hosts the lyrics path touches — the public
// search (track-id resolve), the anonymous-JWT bootstrap, and the pipe GraphQL —
// to canned bodies. redirectTransport rewrites every host to this server, so the
// adapter's hard-coded auth/pipe URLs land here and route by path.
func deezerLyricsServer(jwtHits, pipeHits *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/search/track"):
			w.Write([]byte(`{"data":[{"id":3135556,"title":"Hello","link":"https://www.deezer.com/track/3135556","artist":{"id":75491,"name":"Adele"}}]}`))
		case r.URL.Path == "/login/anonymous":
			if jwtHits != nil {
				atomic.AddInt32(jwtHits, 1)
			}
			w.Write([]byte(`{"jwt":"anon-test-jwt"}`))
		case r.URL.Path == "/api":
			if pipeHits != nil {
				atomic.AddInt32(pipeHits, 1)
			}
			if r.Header.Get("Authorization") != "Bearer anon-test-jwt" {
				http.Error(w, "missing bearer", http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"data":{"track":{"id":"3135556","lyrics":{"id":"2","copyright":"Universal","text":"Hello, it's me\nI was wondering","writers":"Adele Laurie Blue Adkins, Gregory Allen Kurstin","synchronizedLines":[{"lrcTimestamp":"[00:12.34]","line":"Hello, it's me","milliseconds":12340,"duration":2000},{"lrcTimestamp":"[00:15.00]","line":"I was wondering","milliseconds":15000,"duration":2500},{"lrcTimestamp":"","line":"","milliseconds":0,"duration":0}]}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestDeezerLyricsAdapter_ResolveAndLookup(t *testing.T) {
	var jwtHits, pipeHits int32
	server := deezerLyricsServer(&jwtHits, &pipeHits)
	defer server.Close()
	adapter := NewDeezerLyricsAdapter(newTestClient(server.URL))

	id, err := adapter.ResolveTrackID(context.Background(), "Adele", "Hello")
	if err != nil {
		t.Fatalf("ResolveTrackID: %v", err)
	}
	if id != "3135556" {
		t.Fatalf("ResolveTrackID: got %q, want 3135556", id)
	}

	lyrics, err := adapter.Lookup(context.Background(), id)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !strings.HasPrefix(lyrics.Plain, "Hello, it's me") {
		t.Errorf("Plain: got %q", lyrics.Plain)
	}
	if lyrics.Copyright != "Universal" {
		t.Errorf("Copyright: got %q", lyrics.Copyright)
	}
	if len(lyrics.Writers) != 2 || lyrics.Writers[0] != "Adele Laurie Blue Adkins" {
		t.Errorf("Writers: got %v", lyrics.Writers)
	}
	// The empty trailing separator line must be dropped.
	if len(lyrics.SyncedLines) != 2 {
		t.Fatalf("SyncedLines: got %d, want 2", len(lyrics.SyncedLines))
	}
	first := lyrics.SyncedLines[0]
	if first.Timecode != "[00:12.34]" || first.Line != "Hello, it's me" || first.Milliseconds != 12340 || first.Duration != 2000 {
		t.Errorf("first synced line: got %+v", first)
	}
}

func TestDeezerLyricsAdapter_JWTCachedAcrossLookups(t *testing.T) {
	var jwtHits, pipeHits int32
	server := deezerLyricsServer(&jwtHits, &pipeHits)
	defer server.Close()
	adapter := NewDeezerLyricsAdapter(newTestClient(server.URL))

	for range 3 {
		if _, err := adapter.Lookup(context.Background(), "3135556"); err != nil {
			t.Fatalf("Lookup: %v", err)
		}
	}
	if got := atomic.LoadInt32(&jwtHits); got != 1 {
		t.Errorf("anonymous jwt should be bootstrapped once, got %d hits", got)
	}
	if got := atomic.LoadInt32(&pipeHits); got != 3 {
		t.Errorf("expected 3 pipe calls, got %d", got)
	}
}

func TestDeezerLyricsAdapter_SelfHealsOn401(t *testing.T) {
	var pipeHits int32
	// The first stale JWT is rejected; after re-bootstrap the second succeeds.
	tokens := []string{"stale-jwt", "fresh-jwt"}
	var jwtIdx int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/anonymous":
			i := atomic.AddInt32(&jwtIdx, 1) - 1
			w.Write([]byte(`{"jwt":"` + tokens[i] + `"}`))
		case "/api":
			atomic.AddInt32(&pipeHits, 1)
			if r.Header.Get("Authorization") != "Bearer fresh-jwt" {
				http.Error(w, "expired", http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"data":{"track":{"id":"1","lyrics":{"text":"ok","writers":"","synchronizedLines":[]}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter := NewDeezerLyricsAdapter(newTestClient(server.URL))

	lyrics, err := adapter.Lookup(context.Background(), "1")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if lyrics.Plain != "ok" {
		t.Errorf("Plain: got %q after self-heal", lyrics.Plain)
	}
	if got := atomic.LoadInt32(&pipeHits); got != 2 {
		t.Errorf("expected 2 pipe calls (stale + retried), got %d", got)
	}
}

func TestDeezerLyricsAdapter_NoLyricsIsEmptyNotError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/login/anonymous":
			w.Write([]byte(`{"jwt":"anon-test-jwt"}`))
		case "/api":
			// LyricsNotFoundError — structured GraphQL error, null lyrics.
			w.Write([]byte(`{"data":{"track":{"id":"1","lyrics":null}},"errors":[{"message":"LyricsNotFoundError"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter := NewDeezerLyricsAdapter(newTestClient(server.URL))

	lyrics, err := adapter.Lookup(context.Background(), "1")
	if err != nil {
		t.Fatalf("a structured no-lyrics miss must not be an error, got %v", err)
	}
	if !lyrics.IsZero() {
		t.Errorf("expected empty lyrics, got %+v", lyrics)
	}
}

func TestDeezerLyricsAdapter_EmptyTrackIDReturnsEmpty(t *testing.T) {
	adapter := NewDeezerLyricsAdapter(newTestClient("http://unused"))
	lyrics, err := adapter.Lookup(context.Background(), "")
	if err != nil || !lyrics.IsZero() {
		t.Errorf("empty track id should return empty + nil, got %+v err=%v", lyrics, err)
	}
}
