package providers

import (
	"testing"
	"time"
)

func TestClientIDResolver_staleInvalidateNoops(t *testing.T) {
	r := newClientIDResolver(nil)
	r.cached = "fresh"
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
	r := newAppleMusicTokenResolver(nil)
	r.cached = "fresh"
	r.expiry = time.Now().Add(time.Hour)
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
	r := newDeezerJWTResolver(nil)
	r.cached = "fresh"
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
	r := newSpotifyTokenResolver(nil)
	r.cached = fresh
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
	r := newAmazonMusicSessionResolver(nil)
	r.cached = fresh
	r.invalidate(stale)
	if r.cached != fresh {
		t.Fatal("stale invalidate wiped the fresh session")
	}
	r.invalidate(fresh)
	if r.cached != nil {
		t.Fatal("invalidate with the failed session did not clear the cache")
	}
}
