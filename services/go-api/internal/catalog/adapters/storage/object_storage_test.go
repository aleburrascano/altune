package storage

import (
	"context"
	"net/url"
	"strconv"
	"testing"
	"time"

	"altune/go-api/internal/catalog/ports"
)

// Presigning is a local signature computation when the region is configured,
// so these run without a live object store.
func newOfflineObjectStore(t *testing.T) *ObjectStorageAudioStore {
	t.Helper()
	store, err := NewObjectStorageAudioStore("https://objectstorage.invalid", "ak", "sk", "bucket", "us-east-1")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	return store
}

func presignedExpiry(t *testing.T, raw string) time.Duration {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse presigned url: %v", err)
	}
	secs, err := strconv.Atoi(u.Query().Get("X-Amz-Expires"))
	if err != nil {
		t.Fatalf("X-Amz-Expires in %q: %v", raw, err)
	}
	return time.Duration(secs) * time.Second
}

func TestObjectStorageAudioStore_PresignGet_TTLCeiling(t *testing.T) {
	ctx := context.Background()
	store := newOfflineObjectStore(t)

	tests := []struct {
		name string
		ttl  time.Duration
		want time.Duration
	}{
		{name: "within ceiling is honored", ttl: 10 * time.Minute, want: 10 * time.Minute},
		{name: "at ceiling is honored", ttl: ports.MaxPresignTTL, want: ports.MaxPresignTTL},
		{name: "above ceiling is clamped", ttl: 7 * 24 * time.Hour, want: ports.MaxPresignTTL},
		{name: "beyond the s3 maximum is clamped, not rejected", ttl: 365 * 24 * time.Hour, want: ports.MaxPresignTTL},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := store.PresignGet(ctx, "user/artist/album/song.opus", tc.ttl)
			if err != nil {
				t.Fatalf("presign: %v", err)
			}
			if got := presignedExpiry(t, u); got != tc.want {
				t.Errorf("X-Amz-Expires = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestObjectStorageAudioStore_PresignGet_RejectsNonPositiveTTL(t *testing.T) {
	store := newOfflineObjectStore(t)
	for _, ttl := range []time.Duration{0, -time.Minute} {
		if u, err := store.PresignGet(context.Background(), "user/a/b/c.opus", ttl); err == nil {
			t.Errorf("ttl %s: expected error, got url %q", ttl, u)
		}
	}
}
