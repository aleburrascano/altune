package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Concurrency contracts shared by the token/session resolvers:
//
//  1. singleflight collapse — N concurrent cold-start gets trigger exactly one
//     resolve round trip;
//  2. detached resolve ctx — a caller whose ctx is already cancelled must not
//     poison the shared resolve (it runs on its own budget);
//  3. expiry-triggered re-resolve — an expired cached credential is replaced,
//     not returned;
//  4. invalidate storm under concurrency — stale invalidates racing a fresh
//     credential never wipe it.

func TestAmazonMusicSessionResolver_singleflightCollapsesConcurrentGets(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		time.Sleep(50 * time.Millisecond) // hold the resolve open so callers pile up
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deviceId":"d","sessionId":"s","csrf":{"token":"t","rnd":"r","ts":"1"}}`))
	}))
	defer srv.Close()

	r := newAmazonMusicSessionResolver(srv.Client())
	r.configURL = srv.URL

	const callers = 8
	var wg sync.WaitGroup
	sessions := make([]*amazonMusicSession, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sess, err := r.get(context.Background())
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			sessions[i] = sess
		}(i)
	}
	wg.Wait()

	if got := hits.Load(); got != 1 {
		t.Errorf("config.json fetched %d times for %d concurrent callers, want 1 (singleflight)", got, callers)
	}
	for i := 1; i < callers; i++ {
		if sessions[i] != sessions[0] {
			t.Fatalf("caller %d got a different session pointer — waiters must share the winner's resolve", i)
		}
	}
}

func TestClientIDResolver_singleflightCollapsesConcurrentGets(t *testing.T) {
	var homeHits atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".js") {
			_, _ = w.Write([]byte(`client_id:"abcdefabcdefabcdefabcdefabcdef12"`))
			return
		}
		homeHits.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`<script src="` + srv.URL + `/assets/app-1.js"></script>`))
	}))
	defer srv.Close()

	r := newClientIDResolver(srv.Client())
	r.siteURL = srv.URL

	const callers = 8
	var wg sync.WaitGroup
	ids := make([]string, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id, err := r.get(context.Background())
			if err != nil {
				t.Errorf("get: %v", err)
				return
			}
			ids[i] = id
		}(i)
	}
	wg.Wait()

	if got := homeHits.Load(); got != 1 {
		t.Errorf("homepage scraped %d times for %d concurrent callers, want 1 (singleflight)", got, callers)
	}
	for _, id := range ids {
		if id != "abcdefabcdefabcdefabcdefabcdef12" {
			t.Fatalf("id = %q, want the scraped client_id shared by all waiters", id)
		}
	}
}

// A caller whose ctx is already cancelled must still get a session: the shared
// resolve detaches (context.WithoutCancel + its own timeout) so one impatient
// caller can't poison the resolve for every piggybacked waiter.
func TestAmazonMusicSessionResolver_resolveDetachesFromCallerCtx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"deviceId":"d","sessionId":"s","csrf":{"token":"t","rnd":"r","ts":"1"}}`))
	}))
	defer srv.Close()

	r := newAmazonMusicSessionResolver(srv.Client())
	r.configURL = srv.URL

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sess, err := r.get(ctx)
	if err != nil {
		t.Fatalf("get with cancelled caller ctx: %v (resolve must run on its own detached budget)", err)
	}
	if sess.SessionID != "s" {
		t.Errorf("session = %+v, want the resolved one", sess)
	}
}

func TestAppleMusicTokenResolver_expiredTokenTriggersReResolve(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "index~abc123.js") {
			_, _ = w.Write([]byte(`var t = "` + appleMusicFixtureJWT + `";`))
			return
		}
		hits.Add(1)
		_, _ = w.Write([]byte(`<script src="assets/index~abc123.js"></script>`))
	}))
	defer srv.Close()

	r := newAppleMusicTokenResolver(srv.Client())
	r.siteURL = srv.URL
	r.bundleBaseURL = srv.URL + "/"
	r.cached = "expired-token"
	r.expiry = time.Now().Add(-time.Minute) // expired → must NOT be returned

	token, err := r.get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if token == "expired-token" {
		t.Fatal("get returned the expired cached token instead of re-resolving")
	}
	if token != appleMusicFixtureJWT {
		t.Errorf("token = %q, want the freshly scraped JWT", token)
	}
	if hits.Load() != 1 {
		t.Errorf("page scraped %d times, want 1", hits.Load())
	}
}

func TestSpotifyTokenResolver_expiredSessionTriggersReResolve(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/server-time":
			_, _ = w.Write([]byte(`{"serverTime": 1700000000}`))
		case "/token":
			_, _ = w.Write([]byte(`{"accessToken": "fresh-token", "accessTokenExpirationTimestampMs": 99999999999999}`))
		case "/clienttoken":
			_, _ = w.Write([]byte(`{"granted_token": {"token": "fresh-client-token", "expires_after_seconds": 999999}}`))
		}
	}))
	defer srv.Close()

	r := newSpotifyTokenResolver(srv.Client())
	r.serverTimeURL = srv.URL + "/server-time"
	r.accessTokenURL = srv.URL + "/token"
	r.clientTokenURL = srv.URL + "/clienttoken"
	r.cached = &spotifySession{
		accessToken:  "stale-token",
		accessExpiry: time.Now().Add(-time.Minute), // access token expired
		clientToken:  "stale-client-token",
		clientExpiry: time.Now().Add(time.Hour),
	}

	sess, err := r.get(context.Background())
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if sess.accessToken != "fresh-token" || sess.clientToken != "fresh-client-token" {
		t.Errorf("session = %+v, want the freshly resolved pair (expired session must not be reused)", sess)
	}
}

// Stale invalidates racing a fresh credential must never wipe it — run under
// -race this also proves the mutex discipline of get/invalidate.
func TestClientIDResolver_concurrentStaleInvalidateNoops(t *testing.T) {
	r := &clientIDResolver{cached: "fresh"}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.invalidate("stale")
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.mu.Lock()
			_ = r.cached
			r.mu.Unlock()
		}()
	}
	wg.Wait()

	if r.cached != "fresh" {
		t.Fatal("a stale invalidate wiped the fresh client_id under concurrency")
	}
	r.invalidate("fresh")
	if r.cached != "" {
		t.Fatal("invalidate with the failed client_id did not clear the cache")
	}
}

func TestSpotifyTokenResolver_concurrentStaleInvalidateNoops(t *testing.T) {
	fresh := &spotifySession{accessToken: "fresh"}
	stale := &spotifySession{accessToken: "stale"}
	r := &spotifyTokenResolver{cached: fresh}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.invalidate(stale)
		}()
	}
	wg.Wait()

	if r.cached != fresh {
		t.Fatal("a stale invalidate wiped the fresh session under concurrency")
	}
}
