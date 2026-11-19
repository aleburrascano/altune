package storage_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/shared/config"
	"context"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const maxPresignTTL = time.Hour

func newObjectStore(t *testing.T, cfg *config.Config) catalog.AudioURLSigner {
	t.Helper()
	store, err := catalog.FromConfig(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	signer, ok := store.(catalog.AudioURLSigner)
	if !ok {
		t.Fatalf("object storage store %T does not sign URLs", store)
	}
	return signer
}

func newOfflineObjectStore(t *testing.T) catalog.AudioURLSigner {
	t.Helper()
	return newObjectStore(t, &config.Config{
		OCIS3Endpoint:  "https://objectstorage.invalid",
		OCIS3AccessKey: "ak",
		OCIS3SecretKey: "sk",
		OCIS3Bucket:    "bucket",
		OCIS3Region:    "us-east-1",
	})
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
		{name: "at ceiling is honored", ttl: maxPresignTTL, want: maxPresignTTL},
		{name: "above ceiling is clamped", ttl: 7 * 24 * time.Hour, want: maxPresignTTL},
		{name: "beyond the s3 maximum is clamped, not rejected", ttl: 365 * 24 * time.Hour, want: maxPresignTTL},
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

func TestNewObjectStorageAudioStore_BindsConfigFieldsByName(t *testing.T) {
	store := newObjectStore(t, &config.Config{
		OCIS3Endpoint:  "http://objectstorage.invalid:9000/",
		OCIS3AccessKey: "test-access-key",
		OCIS3SecretKey: "test-secret-key",
		OCIS3Bucket:    "test-bucket",
		OCIS3Region:    "eu-test-1",
	})
	raw, err := store.PresignGet(context.Background(), "a/b.opus", time.Minute)
	if err != nil {
		t.Fatalf("presign: %v", err)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse presigned url: %v", err)
	}
	if u.Scheme != "http" || u.Host != "objectstorage.invalid:9000" {
		t.Errorf("endpoint: got %s://%s, want http://objectstorage.invalid:9000", u.Scheme, u.Host)
	}
	if u.Path != "/test-bucket/a/b.opus" {
		t.Errorf("bucket: path = %q, want /test-bucket/a/b.opus", u.Path)
	}
	cred := u.Query().Get("X-Amz-Credential")
	if !strings.HasPrefix(cred, "test-access-key/") || !strings.Contains(cred, "/eu-test-1/s3/") {
		t.Errorf("access key/region: X-Amz-Credential = %q", cred)
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
