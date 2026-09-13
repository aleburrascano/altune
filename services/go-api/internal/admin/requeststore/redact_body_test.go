package requeststore

import (
	"altune/go-api/internal/shared/httputil"
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
		name:    "spotify client token",
		body:    `{"response_type":"RESPONSE_GRANTED_TOKEN_RESPONSE","granted_token":{"token":"` + spotifyClient + `","expires_after_seconds":1209600}}`,
		secrets: []string{spotifyClient},
		keep:    []string{`"expires_after_seconds":1209600`},
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
			req = req.WithContext(httputil.WithCorrelationID(req.Context(), "c1"))
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
