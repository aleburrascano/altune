package audiofiles_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const maxPresignTTL = time.Hour

func newObjectStore(t *testing.T, cfg *config.Config) library.AudioLinkSigner {
	t.Helper()
	store, err := library.NewAudioStoreFromConfig(cfg, slog.New(slog.DiscardHandler), library.NoopFSCallMetrics())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	signer, ok := store.(library.AudioLinkSigner)
	if !ok {
		t.Fatalf("object storage store %T does not sign URLs", store)
	}
	return signer
}

func newOfflineObjectStore(t *testing.T) library.AudioLinkSigner {
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

func newStoreAgainst(t *testing.T, handler http.HandlerFunc) library.AudioLinkSigner {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return newObjectStore(t, &config.Config{
		OCIS3Endpoint:  server.URL,
		OCIS3AccessKey: "ak",
		OCIS3SecretKey: "sk",
		OCIS3Bucket:    "bucket",
		OCIS3Region:    "us-east-1",
	})
}

func TestObjectStorageAudioStore_Delete_RemovesTheObjectFromTheBucket(t *testing.T) {
	var gotMethod, gotPath string
	store := newStoreAgainst(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	err := store.(interface {
		Delete(context.Context, string) error
	}).Delete(context.Background(), "user/a/b/c.opus")

	if err != nil || gotMethod != http.MethodDelete || gotPath != "/bucket/user/a/b/c.opus" {
		t.Fatalf("Delete = %v via %s %s, want nil via DELETE /bucket/user/a/b/c.opus", err, gotMethod, gotPath)
	}
}

func TestObjectStorageAudioStore_Delete_NamesTheObjectWhenTheBucketRefuses(t *testing.T) {
	store := newStoreAgainst(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>no</Message></Error>`))
	})

	err := store.(interface {
		Delete(context.Context, string) error
	}).Delete(context.Background(), "user/a/b/c.opus")

	if err == nil || !strings.Contains(err.Error(), "user/a/b/c.opus") || errors.Unwrap(err) == nil {
		t.Fatalf("Delete = %v, want an error naming user/a/b/c.opus and wrapping the cause", err)
	}
}
