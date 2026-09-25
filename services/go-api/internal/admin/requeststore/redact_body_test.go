package requeststore

import (
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/redact"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	spotifyAccess = "BQDspotifyAccessTokenSecretValue123"
	spotifyClient = "AADspotifyClientTokenSecretValue456"
	amzSession    = "133-7777777-1234567"
	amzCSRF       = "gAAAamzCsrfTokenSecretValue789"
	amzAccess     = "Atna|amzAccessTokenSecretValue000"
	appleJWT      = "eyJhbGciOiJFUzI1NiJ9.eyJpc3MiOiJBTVBXZWJQbGF5In0.c2lnbmF0dXJlLXNlY3JldA"
	appleJWTHead  = "eyJhbGciOiJFUzI1NiJ9."
	discogsToken  = "abcDISCOGSpersonalAccessTokenValue01"
	accessKeyID   = "AKIAIOSFODNN7EXAMPLE"
	genericCred   = "u9kCREDENTIALvalueSecretValue22334455"
	privateKey    = "-----BEGIN PRIVATE KEY-----MIIEvQIBADAN"
	numericToken  = "987654321098765"
	formToken     = "FORMaccessTokenSecretValue778899"
)

// Response bodies shaped like the real token/session flows that reuse the
// traced provider client.
var credentialBodies = []struct {
	name    string
	body    string
	secrets []string
	keep    []string
}{
	{
		name:    "spotify access token",
		body:    `{"clientId":"d8a5ed958d274c2e8ee717e6a4b0971d","accessToken":"` + spotifyAccess + `","accessTokenExpirationTimestampMs":1757770000000,"isAnonymous":true}`,
		secrets: []string{spotifyAccess},
		keep:    []string{`"isAnonymous":true`, `"accessTokenExpirationTimestampMs":1757770000000`},
	},
	{
		// granted_token names a credential, so the whole subtree under it goes:
		// the field kept here is the sibling outside it.
		name:    "spotify client token",
		body:    `{"response_type":"RESPONSE_GRANTED_TOKEN_RESPONSE","granted_token":{"token":"` + spotifyClient + `","expires_after_seconds":1209600}}`,
		secrets: []string{spotifyClient},
		keep:    []string{`"response_type":"RESPONSE_GRANTED_TOKEN_RESPONSE"`},
	},
	{
		name:    "amazon music config.json",
		body:    `{"accessToken":"` + amzAccess + `","csrf":{"token":"` + amzCSRF + `","ts":"1757770000","rnd":"123456"},"deviceId":"dev1","deviceType":"A16ZV8BU3SN1N3","sessionId":"` + amzSession + `","version":"1.0"}`,
		secrets: []string{amzAccess, amzCSRF, amzSession},
		keep:    []string{`"deviceType":"A16ZV8BU3SN1N3"`, `"version":"1.0"`},
	},
	{
		name:    "amazon music escaped headers bundle",
		body:    `{"headers":"{\"x-amzn-authentication\":\"{\\\"interface\\\":\\\"ClientAuthenticationInterface.v1_0.ClientTokenElement\\\",\\\"accessToken\\\":\\\"` + amzAccess + `\\\"}\",\"x-amzn-session-id\":\"` + amzSession + `\"}"}`,
		secrets: []string{amzAccess, amzSession},
	},
	{
		name:    "apple music bundle jwt",
		body:    `const e={devToken:"` + appleJWT + `",storefront:"us"};`,
		secrets: []string{appleJWT},
		keep:    []string{`storefront:"us"`},
	},
	{
		name:    "truncated body mid-object",
		body:    `{"granted_token":{"token":"` + spotifyClient + `","expires_after`,
		secrets: []string{spotifyClient},
	},
	{
		name:    "discogs token field",
		body:    `{"discogs_token":"` + discogsToken + `","username":"crate-digger"}`,
		secrets: []string{discogsToken},
		keep:    []string{`"username":"crate-digger"`},
	},
	{
		name:    "access key id",
		body:    `{"accessKey":"` + accessKeyID + `","region":"eu-west-1"}`,
		secrets: []string{accessKeyID},
		keep:    []string{`"region":"eu-west-1"`},
	},
	{
		name:    "credential and private key fields",
		body:    `{"credential":"` + genericCred + `","private_key":"` + privateKey + `","kid":"k1"}`,
		secrets: []string{genericCred, privateKey},
		keep:    []string{`"kid":"k1"`},
	},
	{
		name:    "token issued as a number",
		body:    `{"access_token":` + numericToken + `,"expires_in":3600}`,
		secrets: []string{numericToken},
		keep:    []string{`"expires_in":3600`},
	},
	{
		name:    "form-encoded token response",
		body:    `access_token=` + formToken + `&token_type=bearer&expires_in=3600`,
		secrets: []string{formToken},
		keep:    []string{`token_type=bearer`, `expires_in=3600`},
	},
	{
		name:    "jwt cut off before its second segment",
		body:    `const e={devToken:"` + appleJWTHead,
		secrets: []string{appleJWTHead},
	},
}

// Names redact.IsSecretKey calls credentials, one per rule of its vocabulary:
// the "ends in token" suffix and each marker it carries.
var sharedSecretKeyNames = []string{
	"token", "accessToken", "access_token", "ACCESS_TOKEN", "discogs_token",
	"refresh_token", "id_token", "client_token", "dev_token",
	"client_secret", "secret", "SECRET_KEY", "password", "passwd",
	"credential", "credentials", "api_key", "apikey", "access_key",
	"supabase_anon_key", "private_key",
}

// The shared vocabulary is the one list of credential names in this codebase:
// a name it knows must not keep its value in a stored body either.
func TestRedactBody_MasksEveryNameTheSharedVocabularyCallsSecret(t *testing.T) {
	const value = "PLAINcredentialValueThatMustNotBeStored"

	for _, name := range sharedSecretKeyNames {
		if !redact.IsSecretKey(name) {
			t.Fatalf("fixture drift: %q is no longer a credential name to the shared redactor", name)
		}
		if got := RedactBody(`{"` + name + `":"` + value + `"}`); strings.Contains(got, value) {
			t.Errorf("%q names a credential but its value was stored: %s", name, got)
		}
	}
}

func assertRedacted(t *testing.T, got string, secrets, keep []string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(got, s) {
			t.Errorf("captured body leaked secret %q: %s", s, got)
		}
	}
	for _, k := range keep {
		if !strings.Contains(got, k) {
			t.Errorf("non-secret content %q lost: %s", k, got)
		}
	}
}

func TestTransport_RedactsCredentialsInCapturedBody(t *testing.T) {
	for _, tc := range credentialBodies {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			base := respWith(tc.body)
			t.Cleanup(func() { _ = base.Body.Close() })
			rt := NewCorrelatedTransport(fakeRT{resp: base}, s)

			resp, err := rt.RoundTrip(reqWithCorr("c1"))
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(got) != tc.body {
				t.Fatalf("caller must receive the unredacted body, got %q", got)
			}

			rec, ok := s.Get("c1")
			if !ok || len(rec.Exchanges) != 1 {
				t.Fatalf("expected one exchange, got %+v", rec)
			}
			assertRedacted(t, rec.Exchanges[0].RespBody, tc.secrets, tc.keep)
		})
	}
}

func TestRerunRecorder_RedactsCredentialsInCapturedBody(t *testing.T) {
	for _, tc := range credentialBodies {
		t.Run(tc.name, func(t *testing.T) {
			base := respWith(tc.body)
			t.Cleanup(func() { _ = base.Body.Close() })
			rr := NewRerunRecorder(fakeRT{resp: base}, 64<<10)
			req, err := http.NewRequest("GET", "https://open.spotify.com/api/token", nil)
			if err != nil {
				t.Fatal(err)
			}
			req = req.WithContext(logging.WithCorrelationID(req.Context(), "c1"))
			resp, err := rr.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if string(got) != tc.body {
				t.Fatalf("caller must receive the unredacted body, got %q", got)
			}
			assertRedacted(t, rr.Exchanges()[0].RespBody, tc.secrets, tc.keep)
		})
	}
}
