package httptrace_test

import (
	"altune/go-api/internal/shared/httptrace"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

const (
	fakeKey         = "0123456789abcdef0123456789abcdef"
	fakeAccessToken = "BQCfakeSpotifyAccessTokenValue0123456789"
	captureLimit    = 1 << 20
	truncatedMarker = "[TRUNCATED: body exceeds capture limit]"
)

type errRT struct{ err error }

func (e errRT) RoundTrip(*http.Request) (*http.Response, error) { return nil, e.err }

type okRT struct{}

func (okRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
}

type bodyRT struct{ body string }

func (b bodyRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(b.body)), Request: req}, nil
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
	rec := httptrace.NewRecorder(errRT{err: errors.New(errMsg)})

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

	ex := onlyExchange(t, rec)
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
	rec := httptrace.NewRecorder(okRT{})
	resp := mustGet(t, rec, "http://fixture.local/?key="+fakeKey)
	defer resp.Body.Close()

	ex := onlyExchange(t, rec)
	if strings.Contains(ex.URL, fakeKey) {
		t.Errorf("Exchange.URL leaked the key: %q", ex.URL)
	}
}

func TestRecorder_RedactsCredentialFieldInResponseBody(t *testing.T) {
	const body = `{"accessToken":"` + fakeAccessToken + `","accessTokenExpirationTimestampMs":1770000000000,"isAnonymous":true}`
	rec := httptrace.NewRecorder(bodyRT{body: body})

	resp := mustGet(t, rec, "https://open.spotify.com/api/token?reason=init")
	defer resp.Body.Close()

	if got := bodyString(t, resp); got != body {
		t.Errorf("the caller's own response body was altered: %q", got)
	}
	ex := onlyExchange(t, rec)
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
	rec := httptrace.NewRecorder(bodyRT{body: body})

	resp := mustGet(t, rec, "https://clienttoken.spotify.com/v1/clienttoken")
	defer resp.Body.Close()

	ex := onlyExchange(t, rec)
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
	rec := httptrace.NewRecorder(bodyRT{body: "{}"})

	resp := mustPost(t, rec, "https://clienttoken.spotify.com/v1/clienttoken", body)
	defer resp.Body.Close()

	ex := onlyExchange(t, rec)
	if strings.Contains(ex.ReqBody, fakeKey) {
		t.Errorf("Exchange.ReqBody leaked the client secret: %q", ex.ReqBody)
	}
	if !strings.Contains(ex.ReqBody, `"client_version":"1.2.3"`) {
		t.Errorf("non-secret request fields lost: %q", ex.ReqBody)
	}
}

func TestRecorder_RedactsCredentialParamInFormRequestBody(t *testing.T) {
	const body = "grant_type=refresh_token&refresh_token=" + fakeKey + "&scope=user-read"
	rec := httptrace.NewRecorder(bodyRT{body: "<html>not json</html>"})

	resp := mustPost(t, rec, "https://accounts.spotify.com/api/token", body)
	defer resp.Body.Close()

	ex := onlyExchange(t, rec)
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
	rec := httptrace.NewRecorder(okRT{})
	recorded := mustGet(t, rec, url)
	defer recorded.Body.Close()

	rep := httptrace.NewReplayer(rec.Exchanges())
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
	rec := httptrace.NewRecorder(bodyRT{body: `{"granted":true}`})
	recorded := mustPost(t, rec, url, reqBody)
	defer recorded.Body.Close()

	rep := httptrace.NewReplayer(rec.Exchanges())
	replayed := mustPost(t, rep, url, reqBody)
	defer replayed.Body.Close()
	if got := bodyString(t, replayed); got != `{"granted":true}` {
		t.Errorf("replay body = %q, want the recorded response", got)
	}
}

func TestRecorder_OversizedResponseIsMarkedTruncatedAndCallerKeepsFullBody(t *testing.T) {
	body := strings.Repeat("a", captureLimit+10)
	rec := httptrace.NewRecorder(bodyRT{body: body})

	resp := mustGet(t, rec, "http://fixture.local/big")
	defer resp.Body.Close()

	if got := bodyString(t, resp); got != body {
		t.Errorf("the caller's response body was cut: got %d bytes, want %d", len(got), len(body))
	}
	ex := onlyExchange(t, rec)
	if ex.RespBody != truncatedMarker {
		t.Errorf("RespBody = %d bytes, want the truncation placeholder", len(ex.RespBody))
	}
	if ex.Err == "" {
		t.Error("a truncated response was not flagged in Err")
	}
}

func TestRecorder_OversizedRequestIsMarkedTruncatedAndForwardedWhole(t *testing.T) {
	body := strings.Repeat("b", captureLimit+10)
	var seen int
	rec := httptrace.NewRecorder(rtFunc(func(req *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(req.Body)
		seen = len(b)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	}))

	resp := mustPost(t, rec, "http://fixture.local/big", body)
	defer resp.Body.Close()

	if seen != len(body) {
		t.Errorf("transport saw %d request bytes, want %d", seen, len(body))
	}
	ex := onlyExchange(t, rec)
	if ex.ReqBody != truncatedMarker || ex.Err == "" {
		t.Errorf("oversized request not marked truncated: body=%d bytes err=%q", len(ex.ReqBody), ex.Err)
	}
}

func TestReplayer_RefusesTruncatedRecording(t *testing.T) {
	rec := httptrace.NewRecorder(bodyRT{body: strings.Repeat("a", captureLimit+1)})
	resp := mustGet(t, rec, "http://fixture.local/big")
	defer resp.Body.Close()

	req, err := http.NewRequest(http.MethodGet, "http://fixture.local/big", nil)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := httptrace.NewReplayer(rec.Exchanges()).RoundTrip(req)
	if replayed != nil {
		_ = replayed.Body.Close()
	}
	if err == nil {
		t.Error("a truncated recording was replayed as a complete response")
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func onlyExchange(t *testing.T, rec *httptrace.Recorder) httptrace.Exchange {
	t.Helper()
	got := rec.Exchanges()
	if len(got) != 1 {
		t.Fatalf("recorded %d exchanges, want 1", len(got))
	}
	return got[0]
}

func mustGet(t *testing.T, rt http.RoundTripper, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip %s: %v", url, err)
	}
	return resp
}

func bodyString(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func TestReplayer_ReturnsRecordedResponse(t *testing.T) {
	r := httptrace.NewReplayer([]httptrace.Exchange{
		{Method: "GET", URL: "https://api.example.com/search?q=foo", Status: 200, RespBody: `{"hit":"foo"}`},
	})

	resp := mustGet(t, r, "https://api.example.com/search?q=foo")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := bodyString(t, resp); got != `{"hit":"foo"}` {
		t.Errorf("body = %q, want the recorded body", got)
	}
}

func TestReplayer_FIFOForRepeatedKey(t *testing.T) {
	r := httptrace.NewReplayer([]httptrace.Exchange{
		{Method: "GET", URL: "https://api.example.com/page", Status: 200, RespBody: `first`},
		{Method: "GET", URL: "https://api.example.com/page", Status: 200, RespBody: `second`},
	})

	first := mustGet(t, r, "https://api.example.com/page")
	defer first.Body.Close()
	if got := bodyString(t, first); got != "first" {
		t.Errorf("first replay = %q, want first", got)
	}
	second := mustGet(t, r, "https://api.example.com/page")
	defer second.Body.Close()
	if got := bodyString(t, second); got != "second" {
		t.Errorf("second replay = %q, want second", got)
	}
}

func TestReplayer_MissIsHardError(t *testing.T) {
	r := httptrace.NewReplayer(nil)

	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/missing", nil)
	resp, err := r.RoundTrip(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected an error for an unrecorded request, got nil")
	}
}

func TestReplayer_RemainingTracksUnreplayed(t *testing.T) {
	r := httptrace.NewReplayer([]httptrace.Exchange{
		{Method: "GET", URL: "https://api.example.com/a", Status: 200, RespBody: `a`},
		{Method: "GET", URL: "https://api.example.com/b", Status: 200, RespBody: `b`},
	})
	if r.Remaining() != 2 {
		t.Fatalf("Remaining before = %d, want 2", r.Remaining())
	}
	resp := mustGet(t, r, "https://api.example.com/a")
	defer resp.Body.Close()
	_ = bodyString(t, resp)
	if r.Remaining() != 1 {
		t.Errorf("Remaining after one replay = %d, want 1", r.Remaining())
	}
}

func TestReplayer_RecordedTransportErrorReplaysAsError(t *testing.T) {
	r := httptrace.NewReplayer([]httptrace.Exchange{
		{Method: "GET", URL: "https://api.example.com/down", Err: "connection refused"},
	})
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/down", nil)
	resp, err := r.RoundTrip(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected the recorded transport error to replay as an error")
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

func TestReplayer_RejectsOversizedRequestBody(t *testing.T) {
	r := httptrace.NewReplayer(nil)
	body := strings.Repeat("a", captureLimit+1)
	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/x", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := r.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want body-exceeds error", err)
	}
}

func TestReplayer_SurfacesRequestBodyReadError(t *testing.T) {
	r := httptrace.NewReplayer(nil)
	boom := errors.New("boom")
	req, err := http.NewRequest(http.MethodPost, "https://api.example.com/x", io.NopCloser(failingReader{err: boom}))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := r.RoundTrip(req)
	if resp != nil {
		resp.Body.Close()
	}
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapped %v", err, boom)
	}
}
