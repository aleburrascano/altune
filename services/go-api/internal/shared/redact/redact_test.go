package redact

import (
	"strings"
	"testing"
)

const lastfmKey = "0123456789abcdef0123456789abcdef"

func TestSecrets_masksValuesKeepsShape(t *testing.T) {
	in := "https://ws.audioscrobbler.com/2.0/?method=track.search&api_key=" + lastfmKey + "&format=json"
	got := Secrets(in)
	if strings.Contains(got, lastfmKey) {
		t.Fatalf("raw key leaked: %q", got)
	}
	if !strings.Contains(got, "api_key=REDACTED") {
		t.Errorf("api_key value not redacted: %q", got)
	}
	if !strings.Contains(got, "method=track.search") || !strings.Contains(got, "format=json") {
		t.Errorf("non-secret params or path lost: %q", got)
	}
}

func TestSecrets_masksSoundCloudClientID(t *testing.T) {
	const clientID = "ScrapedClientIdValue0123456789AB"
	in := `soundcloud api-v2: Get "https://api-v2.soundcloud.com/users/1/toptracks?client_id=` + clientID + `&limit=50": dial tcp: refused`
	got := Secrets(in)
	if strings.Contains(got, clientID) {
		t.Fatalf("client_id leaked: %q", got)
	}
	for _, kept := range []string{"client_id=REDACTED", "limit=50", "/users/1/toptracks"} {
		if !strings.Contains(got, kept) {
			t.Errorf("missing %q in %q", kept, got)
		}
	}
}
