package logging

import (
	"strings"
	"testing"
)

// TestRingHandler_RedactsConfigSecretFields reproduces the denylist gap from
// issue #643: before the fix, only the literal "query" key was stripped, so an
// attr keyed like any real Config secret field (redis_url, api_key, tokens,
// access/secret keys) reached the live-tailing ring buffer unredacted.
func TestRingHandler_RedactsConfigSecretFields(t *testing.T) {
	logger, ring := newCaptureLogger(t, 20)

	const secret = "s3cr3t-value-should-never-surface"
	// Log keys mirror how each Config secret field snake-cases when logged.
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

// TestRingHandler_RedactsCredentialURLInErrorAttr covers a credential URL that
// leaks through a generic "error" attr — the value, not the key, is the tell.
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

// TestRedaction_KeepsNonSecretLookalikeKeys guards against over-redaction: keys
// that merely resemble secret vocabulary (dedup_key, idempotency_key,
// image_url, token_count) are not secrets and must survive.
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
