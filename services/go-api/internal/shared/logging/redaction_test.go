package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

func TestRingHandler_RedactsConfigSecretFields(t *testing.T) {
	logger, ring := newCaptureLogger(t, 20)

	const secret = "s3cr3t-value-should-never-surface"
	secretKeys := []string{
		"supabase_anon_key",
		"redis_url",
		"lastfm_api_key",
		"fanarttv_api_key",
		"genius_access_token",
		"discogs_token",
		"oci_s3_access_key",
		"oci_s3_secret_key",
		"acoustid_api_key",
		"github_issue_token",
		"database_url",
	}

	for _, key := range secretKeys {
		val := secret
		if strings.HasSuffix(key, "url") {
			val = "postgres://user:" + secret + "@db.internal:5432/altune"
		}
		logger.Info("config.loaded", key, val, "corr_id", "abc123")
	}

	for _, rec := range ring.Snapshot() {
		for k, v := range rec.Attrs {
			if strings.Contains(v, secret) {
				t.Errorf("secret leaked into ring attr %q = %q", k, v)
			}
		}
		if rec.Attrs["corr_id"] != "abc123" {
			t.Errorf("non-sensitive attr dropped: corr_id = %q, want abc123", rec.Attrs["corr_id"])
		}
	}
}

func TestRingHandler_RedactsCredentialURLInErrorAttr(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

	const secret = "hunter2pw"
	logger.Error("redis.dial.failed",
		"error", "dial redis://default:"+secret+"@cache:6379: timeout",
		"attempt", 3,
	)

	snap := ring.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	for k, v := range snap[0].Attrs {
		if strings.Contains(v, secret) {
			t.Errorf("credential URL leaked into ring attr %q = %q", k, v)
		}
	}
	if snap[0].Attrs["attempt"] != "3" {
		t.Errorf("non-sensitive attr dropped: attempt = %q, want 3", snap[0].Attrs["attempt"])
	}
}

func TestRedaction_KeepsNonSecretLookalikeKeys(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

	logger.Info("work",
		"dedup_key", "abc",
		"idempotency_key", "def",
		"favorite_key", "ghi",
		"image_url", "https://cdn.example.com/art.jpg",
		"token_count", 42,
	)

	snap := ring.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	for _, want := range []string{"dedup_key", "idempotency_key", "favorite_key", "image_url", "token_count"} {
		if _, ok := snap[0].Attrs[want]; !ok {
			t.Errorf("non-secret attr %q was over-redacted", want)
		}
	}
}

func TestRedaction_MasksPathsInFailureTextOnly(t *testing.T) {
	logger, ring := newCaptureLogger(t, 10)

	logger.Error("acquire.failed",
		"error", &fs.PathError{Op: "open", Path: "/srv/music/x.flac", Err: errors.New("nope")},
		"panic", "boom at /home/ubuntu/secret/x.go",
		"stack", "goroutine 1:\n\t/home/ubuntu/secret/x.go:12 +0x1",
		"cause", fmt.Errorf("wrap: %w", &fs.PathError{Op: "read", Path: "/srv/music/y.flac", Err: errors.New("bad")}),
		"path", "/v1/feedback",
	)

	snap := ring.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1", len(snap))
	}
	attrs := snap[0].Attrs
	for _, k := range []string{"error", "panic", "stack", "cause"} {
		if strings.Contains(attrs[k], "/srv/") || strings.Contains(attrs[k], "/home/") {
			t.Errorf("path leaked in %q = %q", k, attrs[k])
		}
		if !strings.Contains(attrs[k], "[REDACTED]") {
			t.Errorf("%q = %q, want a masked path", k, attrs[k])
		}
	}
	if attrs["path"] != "/v1/feedback" {
		t.Errorf("path = %q, want /v1/feedback unchanged", attrs["path"])
	}
}
