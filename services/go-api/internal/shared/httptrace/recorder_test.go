package httptrace

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	fakeKey         = "0123456789abcdef0123456789abcdef"
	fakeAccessToken = "BQCfakeSpotifyAccessTokenValue0123456789"
)

type errRT struct{ err error }

func (e errRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

type okRT struct{}

func (okRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
}

type bodyRT struct{ body string }

func (b bodyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(b.body)), Request: req}, nil
}

func mustPost(t *testing.T, rt http.RoundTripper, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip %s: %v", url, err)
	}
	return resp
}

func TestRecorder_RedactsSecretInURLAndErr(t *testing.T) {
	errMsg := `Get "https://ws.audioscrobbler.com/2.0/?api_key=` + fakeKey + `&method=x": dial tcp: i/o timeout`
	rec := NewRecorder(errRT{err: errors.New(errMsg)})

	req, err := http.NewRequest(http.MethodGet, "https://ws.audioscrobbler.com/2.0/?method=track.search&api_key="+fakeKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, rtErr := rec.RoundTrip(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	if rtErr == nil {
		t.Fatal("expected the transport error to propagate")
	}

	ex := rec.Exchanges()[0]
	if strings.Contains(ex.URL, fakeKey) {
		t.Errorf("Exchange.URL leaked the api key: %q", ex.URL)
	}
	if strings.Contains(ex.Err, fakeKey) {
		t.Errorf("Exchange.Err leaked the api key: %q", ex.Err)
	}
	if !strings.Contains(ex.URL, "api_key=REDACTED") || !strings.Contains(ex.URL, "method=track.search") {
		t.Errorf("URL shape not kept: %q", ex.URL)
	}
}

func TestRecorder_RedactsSecretInURLOnSuccess(t *testing.T) {
	rec := NewRecorder(okRT{})
	resp := mustGet(t, rec, "http://fixture.local/?key="+fakeKey)
	defer resp.Body.Close()

	ex := rec.Exchanges()[0]
	if strings.Contains(ex.URL, fakeKey) {
		t.Errorf("Exchange.URL leaked the key: %q", ex.URL)
	}
}

func TestRecorder_RedactsCredentialFieldInResponseBody(t *testing.T) {
	const body = `{"accessToken":"` + fakeAccessToken + `","accessTokenExpirationTimestampMs":1770000000000,"isAnonymous":true}`
	rec := NewRecorder(bodyRT{body: body})

	resp := mustGet(t, rec, "https://open.spotify.com/api/token?reason=init")
	defer resp.Body.Close()

	if got := bodyString(t, resp); got != body {
		t.Errorf("the caller's own response body was altered: %q", got)
	}
	ex := rec.Exchanges()[0]
	if strings.Contains(ex.RespBody, fakeAccessToken) {
		t.Errorf("Exchange.RespBody leaked the access token: %q", ex.RespBody)
	}
	if !strings.Contains(ex.RespBody, `"accessToken":"REDACTED"`) {
		t.Errorf("accessToken not masked in place: %q", ex.RespBody)
	}
	if !strings.Contains(ex.RespBody, `"accessTokenExpirationTimestampMs":1770000000000`) {
		t.Errorf("non-secret fields or number shape lost: %q", ex.RespBody)
	}
}

func TestRecorder_RedactsCredentialFieldsAtAnyDepth(t *testing.T) {
	const body = `{"granted_token":{"pass":"` + fakeAccessToken + `","expires_after_seconds":1209600},` +
		`"clients":[{"name":"web","api_key":"` + fakeKey + `"}]}`
	rec := NewRecorder(bodyRT{body: body})

	resp := mustGet(t, rec, "https://clienttoken.spotify.com/v1/clienttoken")
	defer resp.Body.Close()

	ex := rec.Exchanges()[0]
	for _, leaked := range []string{fakeAccessToken, fakeKey} {
		if strings.Contains(ex.RespBody, leaked) {
			t.Errorf("Exchange.RespBody leaked %q: %s", leaked, ex.RespBody)
		}
	}
	if !strings.Contains(ex.RespBody, `"granted_token":"REDACTED"`) {
		t.Errorf("the credential-named subtree was walked instead of dropped: %q", ex.RespBody)
	}
	if !strings.Contains(ex.RespBody, `"name":"web"`) {
		t.Errorf("non-secret fields beside it were lost: %q", ex.RespBody)
	}
}

func TestRecorder_RedactsCredentialFieldInRequestBody(t *testing.T) {
	const body = `{"client_data":{"client_secret":"` + fakeKey + `","client_version":"1.2.3"}}`
	rec := NewRecorder(bodyRT{body: "{}"})

	resp := mustPost(t, rec, "https://clienttoken.spotify.com/v1/clienttoken", body)
	defer resp.Body.Close()

	ex := rec.Exchanges()[0]
	if strings.Contains(ex.ReqBody, fakeKey) {
		t.Errorf("Exchange.ReqBody leaked the client secret: %q", ex.ReqBody)
	}
	if !strings.Contains(ex.ReqBody, `"client_version":"1.2.3"`) {
		t.Errorf("non-secret request fields lost: %q", ex.ReqBody)
	}
}

func TestRecorder_RedactsCredentialParamInFormRequestBody(t *testing.T) {
	const body = "grant_type=refresh_token&refresh_token=" + fakeKey + "&scope=user-read"
	rec := NewRecorder(bodyRT{body: "<html>not json</html>"})

	resp := mustPost(t, rec, "https://accounts.spotify.com/api/token", body)
	defer resp.Body.Close()

	ex := rec.Exchanges()[0]
	if strings.Contains(ex.ReqBody, fakeKey) {
		t.Errorf("Exchange.ReqBody leaked the refresh token: %q", ex.ReqBody)
	}
	if !strings.Contains(ex.ReqBody, "grant_type=refresh_token") || !strings.Contains(ex.ReqBody, "scope=user-read") {
		t.Errorf("non-secret form params lost: %q", ex.ReqBody)
	}
	if ex.RespBody != "<html>not json</html>" {
		t.Errorf("a non-JSON response body was not kept verbatim: %q", ex.RespBody)
	}
}

func TestReplayer_MatchesRedactedFixtureForKeyedRequest(t *testing.T) {
	const url = "https://ws.audioscrobbler.com/2.0/?method=x&api_key=" + fakeKey
	rec := NewRecorder(okRT{})
	recorded := mustGet(t, rec, url)
	defer recorded.Body.Close()

	rep := NewReplayer(rec.Exchanges())
	replayed := mustGet(t, rep, url)
	defer replayed.Body.Close()
	if got := bodyString(t, replayed); got != "ok" {
		t.Errorf("replay body = %q, want ok", got)
	}
}

func TestReplayer_MatchesFixtureRecordedWithScrubbedRequestBody(t *testing.T) {
	const (
		url     = "https://clienttoken.spotify.com/v1/clienttoken"
		reqBody = `{"client_data":{"client_secret":"` + fakeKey + `","client_version":"1.2.3"}}`
	)
	rec := NewRecorder(bodyRT{body: `{"granted":true}`})
	recorded := mustPost(t, rec, url, reqBody)
	defer recorded.Body.Close()

	rep := NewReplayer(rec.Exchanges())
	replayed := mustPost(t, rep, url, reqBody)
	defer replayed.Body.Close()
	if got := bodyString(t, replayed); got != `{"granted":true}` {
		t.Errorf("replay body = %q, want the recorded response", got)
	}
}
