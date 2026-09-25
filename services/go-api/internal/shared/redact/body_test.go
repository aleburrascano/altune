package redact

import (
	"strings"
	"testing"
)

const bearer = "BQCliveProviderBearerToken0123456789"

func TestSecretsInBody_masksCredentialFieldsAtAnyDepth(t *testing.T) {
	in := `{"accessToken":"` + bearer + `","pages":[{"api_key":"` + bearer + `","title":"Blue"}],` +
		`"nested":{"deep":{"refresh_token":"` + bearer + `"}},"count":7}`

	got := SecretsInBody(in)

	if strings.Contains(got, bearer) {
		t.Fatalf("a credential survived the walk: %s", got)
	}
	for _, kept := range []string{`"title":"Blue"`, `"count":7`} {
		if !strings.Contains(got, kept) {
			t.Errorf("non-secret field %s lost: %s", kept, got)
		}
	}
}

// Case is the provider's choice, not ours: ACCESS_TOKEN, accessToken and
// Access-Token name the same credential.
func TestSecretsInBody_masksCredentialFieldsWhateverTheCase(t *testing.T) {
	in := `{"ACCESS_TOKEN":"` + bearer + `","Api_Key":"` + bearer + `","ClientSecret":"` + bearer + `"}`

	got := SecretsInBody(in)

	if strings.Contains(got, bearer) {
		t.Errorf("a differently-cased credential name escaped: %s", got)
	}
}

// A body truncated by a read error never parses, and the recorder stores it
// anyway — the text fallback has to catch the credential the walk could not.
func TestSecretsInBody_masksCredentialsInUndecodableJSON(t *testing.T) {
	cases := map[string]string{
		"truncated":       `{"accessToken":"` + bearer + `","expires`,
		"newline-delim":   `{"id":1}` + "\n" + `{"refresh_token":"` + bearer + `"}`,
		"inside a script": `<script>window.__data={"client_secret":"` + bearer + `"};</script>`,
		"form-encoded":    `grant_type=refresh_token&refresh_token=` + bearer + `&scope=read`,
	}

	for name, in := range cases {
		got := SecretsInBody(in)
		if strings.Contains(got, bearer) {
			t.Errorf("%s body leaked the credential: %s", name, got)
		}
	}
}

func TestSecretsInBody_keepsBodiesThatCarryNoCredentialByteForByte(t *testing.T) {
	cases := []string{
		``,
		`not json at all`,
		`<html><body>rate limited</body></html>`,
		`{"tracks":[{"title":"Blue","id":"12.0"}],"total":1,"ok":true,"next":null}`,
		`[1,2,3]`,
	}

	for _, in := range cases {
		if got := SecretsInBody(in); got != in {
			t.Errorf("body rewritten with nothing to mask:\n in: %q\nout: %q", in, got)
		}
	}
}

// Replay match keys are built from an already-scrubbed recorded body and a live
// raw one, so scrubbing twice must equal scrubbing once.
func TestSecretsInBody_isIdempotent(t *testing.T) {
	cases := []string{
		`{"accessToken":"` + bearer + `","q":"blue"}`,
		`{"a":{"token":"` + bearer + `"},"n":1770000000000}`,
		`refresh_token=` + bearer + `&scope=read`,
		`{"accessToken":"` + bearer,
	}

	for _, in := range cases {
		once := SecretsInBody(in)
		if twice := SecretsInBody(once); twice != once {
			t.Errorf("not idempotent for %q:\nonce:  %q\ntwice: %q", in, once, twice)
		}
	}
}

func TestSecretsInBody_masksCredentialParamsInsideStringValues(t *testing.T) {
	in := `{"next":"https://api.example.com/page?cursor=2&api_key=` + bearer + `"}`

	got := SecretsInBody(in)

	if strings.Contains(got, bearer) {
		t.Errorf("a credential inside a string value leaked: %s", got)
	}
	if !strings.Contains(got, "cursor=2") {
		t.Errorf("the rest of the URL was lost: %s", got)
	}
}

// A hostile provider can answer with arbitrary nesting; the walk must degrade
// rather than exhaust the stack, both under and over the decoder's depth limit.
func TestSecretsInBody_survivesPathologicalNesting(t *testing.T) {
	for _, depth := range []int{5_000, 20_000} {
		nested := strings.Repeat("[", depth) + strings.Repeat("]", depth)
		if got := SecretsInBody(nested); got != nested {
			t.Errorf("depth %d body rewritten with nothing to mask (len %d)", depth, len(got))
		}
	}
}

func TestIsSecretKey_keepsNonSecretLookalikeNames(t *testing.T) {
	for _, name := range []string{"dedup_key", "idempotency_key", "token_count", "image_url", "keywords", "author", "totpVer"} {
		if IsSecretKey(name) {
			t.Errorf("%q is not a credential name but was treated as one", name)
		}
	}
	for _, name := range []string{"accessToken", "access_token", "client_secret", "refresh_token", "api_key", "supabase_anon_key", "password"} {
		if !IsSecretKey(name) {
			t.Errorf("%q names a credential but was not treated as one", name)
		}
	}
}

func TestSecretsInBodyMasksCredentialAfterStrayClosingByte(t *testing.T) {
	for _, body := range []string{
		`{"a":1}] "password":"hunter2"`,
		`{"a":1}} "password":"hunter2"`,
	} {
		if got := SecretsInBody(body); strings.Contains(got, "hunter2") {
			t.Errorf("SecretsInBody(%q) = %q, leaks credential", body, got)
		}
	}
}
