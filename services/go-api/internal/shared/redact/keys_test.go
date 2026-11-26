package redact_test

import (
	"altune/go-api/internal/shared/redact"
	"testing"
)

func TestIsSecretKey_keepsNonSecretLookalikeNames(t *testing.T) {
	for _, name := range []string{"dedup_key", "idempotency_key", "token_count", "image_url", "keywords", "author", "totpVer"} {
		if redact.IsSecretKey(name) {
			t.Errorf("%q is not a credential name but was treated as one", name)
		}
	}
	for _, name := range []string{"accessToken", "access_token", "client_secret", "refresh_token", "api_key", "supabase_anon_key", "password", "pwd", "key", "ACCESS_TOKEN", "Api_Key", "ClientSecret"} {
		if !redact.IsSecretKey(name) {
			t.Errorf("%q names a credential but was not treated as one", name)
		}
	}
}
