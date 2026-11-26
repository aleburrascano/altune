package redact_test

import (
	"altune/go-api/internal/shared/redact"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

const bearer = "BQCliveProviderBearerToken0123456789"

func TestSecretsInBody_masksCredentialFieldsAtAnyDepth(t *testing.T) {
	in := `{"accessToken":"` + bearer + `","pages":[{"api_key":"` + bearer + `","title":"Blue"}],` +
		`"nested":{"deep":{"refresh_token":"` + bearer + `"}},"count":7}`

	got := secretsInBody(in)

	if strings.Contains(got, bearer) {
		t.Fatalf("a credential survived the walk: %s", got)
	}
	for _, kept := range []string{`"title":"Blue"`, `"count":7`} {
		if !strings.Contains(got, kept) {
			t.Errorf("non-secret field %s lost: %s", kept, got)
		}
	}
}

func TestSecretsInBody_masksCredentialFieldsWhateverTheCase(t *testing.T) {
	in := `{"ACCESS_TOKEN":"` + bearer + `","Api_Key":"` + bearer + `","ClientSecret":"` + bearer + `"}`

	got := secretsInBody(in)

	if strings.Contains(got, bearer) {
		t.Errorf("a differently-cased credential name escaped: %s", got)
	}
}

func TestSecretsInBody_masksACredentialFieldWhateverItsValueType(t *testing.T) {
	got := secretsInBody(`{"password":123456789,"token":{"inner":"x"},"title":"Blue"}`)

	for _, leaked := range []string{"123456789", "inner"} {
		if strings.Contains(got, leaked) {
			t.Errorf("a non-string credential value %s leaked: %s", leaked, got)
		}
	}
	if !strings.Contains(got, `"title":"Blue"`) {
		t.Errorf("non-secret field lost: %s", got)
	}
}

func TestSecretsInBody_masksACredentialThatOnlyAnArrayHolds(t *testing.T) {
	for _, in := range []string{
		`[{"api_key":"` + bearer + `"}]`,
		`{"pages":[{"api_key":"` + bearer + `"}],"title":"Blue"}`,
	} {
		if got := secretsInBody(in); strings.Contains(got, bearer) {
			t.Errorf("SecretsInBody(%q) = %q, leaks the credential", in, got)
		}
	}
}

func TestSecretsInBody_masksCredentialsInUndecodableJSON(t *testing.T) {
	cases := map[string]string{
		"truncated":       `{"accessToken":"` + bearer + `","expires`,
		"newline-delim":   `{"id":1}` + "\n" + `{"refresh_token":"` + bearer + `"}`,
		"inside a script": `<script>window.__data={"client_secret":"` + bearer + `"};</script>`,
		"form-encoded":    `grant_type=refresh_token&refresh_token=` + bearer + `&scope=read`,
	}

	for name, in := range cases {
		got := secretsInBody(in)
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
		if got := secretsInBody(in); got != in {
			t.Errorf("body rewritten with nothing to mask:\n in: %q\nout: %q", in, got)
		}
	}
}

func TestSecretsInBody_isIdempotent(t *testing.T) {
	cases := []string{
		`{"accessToken":"` + bearer + `","q":"blue"}`,
		`{"a":{"token":"` + bearer + `"},"n":1770000000000}`,
		`refresh_token=` + bearer + `&scope=read`,
		`{"accessToken":"` + bearer,
	}

	for _, in := range cases {
		once := secretsInBody(in)
		if twice := secretsInBody(once); twice != once {
			t.Errorf("not idempotent for %q:\nonce:  %q\ntwice: %q", in, once, twice)
		}
	}
}

func TestSecretsInBody_masksCredentialParamsInsideStringValues(t *testing.T) {
	in := `{"next":"https://api.example.com/page?cursor=2&api_key=` + bearer + `"}`

	got := secretsInBody(in)

	if strings.Contains(got, bearer) {
		t.Errorf("a credential inside a string value leaked: %s", got)
	}
	if !strings.Contains(got, "cursor=2") {
		t.Errorf("the rest of the URL was lost: %s", got)
	}
}

func TestSecretsInBody_survivesPathologicalNesting(t *testing.T) {
	for _, depth := range []int{5_000, 20_000} {
		nested := strings.Repeat("[", depth) + strings.Repeat("]", depth)
		if got := secretsInBody(nested); got != nested {
			t.Errorf("depth %d body rewritten with nothing to mask (len %d)", depth, len(got))
		}
	}
}

func TestIsSecretKey_keepsNonSecretLookalikeNames(t *testing.T) {
	for _, name := range []string{"dedup_key", "idempotency_key", "token_count", "image_url", "keywords", "author", "totpVer"} {
		if redact.IsSecretKey(name) {
			t.Errorf("%q is not a credential name but was treated as one", name)
		}
	}
	for _, name := range []string{"accessToken", "access_token", "client_secret", "refresh_token", "api_key", "supabase_anon_key", "password"} {
		if !redact.IsSecretKey(name) {
			t.Errorf("%q names a credential but was not treated as one", name)
		}
	}
}

func TestSecretsInBody_masksExactNameAndMarkerCredentials(t *testing.T) {
	got := secretsInBody(`{"client_id":"a","jwt":"b","pwd":"c","key":"d","keywords":"kw","author":"au","dedup_key":"dk"}`)
	for _, leaked := range []string{`"a"`, `"b"`, `"c"`, `"d"`} {
		if strings.Contains(got, leaked) {
			t.Errorf("credential value %s leaked: %s", leaked, got)
		}
	}
	for _, kept := range []string{`"kw"`, `"au"`, `"dk"`} {
		if !strings.Contains(got, kept) {
			t.Errorf("non-secret value %s was masked: %s", kept, got)
		}
	}
}

func TestSecretsInBodyMasksCredentialAfterStrayClosingByte(t *testing.T) {
	for _, body := range []string{
		`{"a":1}] "password":"hunter2"`,
		`{"a":1}} "password":"hunter2"`,
	} {
		if got := secretsInBody(body); strings.Contains(got, "hunter2") {
			t.Errorf("SecretsInBody(%q) = %q, leaks credential", body, got)
		}
	}
}

func secretsInBody(body string) string {
	scrubbed, isJSON := scrubbedJSON(body)
	if !isJSON {
		return redact.Secrets(body)
	}
	return scrubbed
}

func scrubbedJSON(body string) (string, bool) {
	document, isJSON := decodedJSONDocument(body)
	if !isJSON {
		return "", false
	}
	scrubbed, masked := withoutCredentials(document)
	if !masked {
		return body, true
	}
	return encodedOrMasked(scrubbed), true
}

func decodedJSONDocument(body string) (any, bool) {
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	var document any
	if err := decoder.Decode(&document); err != nil {
		return nil, false
	}
	return document, decoder.Decode(&struct{}{}) == io.EOF
}

func withoutCredentials(v any) (any, bool) {
	switch value := v.(type) {
	case map[string]any:
		return maskedFields(value)
	case []any:
		return maskedItems(value)
	case string:
		masked := redact.Secrets(value)
		return masked, masked != value
	}
	return v, false
}

func maskedFields(fields map[string]any) (map[string]any, bool) {
	masked := false
	for name, v := range fields {
		if redact.IsSecretKey(name) {
			fields[name] = redact.Mask
			masked = true
			continue
		}
		scrubbed, fieldMasked := withoutCredentials(v)
		fields[name] = scrubbed
		masked = masked || fieldMasked
	}
	return fields, masked
}

func maskedItems(items []any) ([]any, bool) {
	masked := false
	for i, item := range items {
		scrubbed, itemMasked := withoutCredentials(item)
		items[i] = scrubbed
		masked = masked || itemMasked
	}
	return items, masked
}

func encodedOrMasked(v any) string {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(v); err != nil {
		return redact.Mask
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
