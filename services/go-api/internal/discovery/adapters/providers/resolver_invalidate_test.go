package providers

import (
	"testing"
	"time"
)

// A stale invalidate — a second 401 handler racing the first — must not wipe
// the fresh credential the first re-resolve just cached (invalidate storm);
// only the credential the caller actually failed with is dropped.

func TestClientIDResolver_staleInvalidateNoops(t *testing.T) {
	r := &clientIDResolver{cached: "fresh"}
	r.invalidate("stale")
	if r.cached != "fresh" {
		t.Fatal("stale invalidate wiped the fresh client_id")
	}
	r.invalidate("fresh")
	if r.cached != "" {
		t.Fatal("invalidate with the failed client_id did not clear the cache")
	}
}

func TestAppleMusicTokenResolver_staleInvalidateNoops(t *testing.T) {
	r := &appleMusicTokenResolver{cached: "fresh", expiry: time.Now().Add(time.Hour)}
	r.invalidate("stale")
	if r.cached != "fresh" {
		t.Fatal("stale invalidate wiped the fresh token")
	}
	r.invalidate("fresh")
	if r.cached != "" {
		t.Fatal("invalidate with the failed token did not clear the cache")
	}
}

func TestDeezerJWTResolver_staleInvalidateNoops(t *testing.T) {
	r := &deezerJWTResolver{cached: "fresh"}
	r.invalidate("stale")
	if r.cached != "fresh" {
		t.Fatal("stale invalidate wiped the fresh jwt")
	}
	r.invalidate("fresh")
	if r.cached != "" {
		t.Fatal("invalidate with the failed jwt did not clear the cache")
	}
}

func TestSpotifyTokenResolver_staleInvalidateNoops(t *testing.T) {
	fresh := &spotifySession{accessToken: "fresh"}
	stale := &spotifySession{accessToken: "stale"}
	r := &spotifyTokenResolver{cached: fresh}
	r.invalidate(stale)
	if r.cached != fresh {
		t.Fatal("stale invalidate wiped the fresh session")
	}
	r.invalidate(fresh)
	if r.cached != nil {
		t.Fatal("invalidate with the failed session did not clear the cache")
	}
}

func TestAmazonMusicSessionResolver_staleInvalidateNoops(t *testing.T) {
	fresh := &amazonMusicSession{SessionID: "fresh"}
	stale := &amazonMusicSession{SessionID: "stale"}
	r := &amazonMusicSessionResolver{cached: fresh}
	r.invalidate(stale)
	if r.cached != fresh {
		t.Fatal("stale invalidate wiped the fresh session")
	}
	r.invalidate(fresh)
	if r.cached != nil {
		t.Fatal("invalidate with the failed session did not clear the cache")
	}
}
