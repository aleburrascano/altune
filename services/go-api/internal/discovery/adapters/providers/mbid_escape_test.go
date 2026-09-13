package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// hostileMBID carries every URL-special character that could restructure an
// outbound request if interpolated raw: a path separator, a query start and a
// fragment start. MBIDs come verbatim from third-party JSON (issue #572).
const hostileMBID = "a/b?c#d"

// capturingClient records the outbound request and answers with an empty 404,
// so nothing leaves the process.
func capturingClient(capture **http.Request) *http.Client {
	return &http.Client{Transport: fakeRoundTripper{fn: func(r *http.Request) (*http.Response, error) {
		*capture = r
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     make(http.Header),
		}, nil
	}}}
}

func TestCoverArtArchiveResolver_EscapesMBIDInRequestURL(t *testing.T) {
	var got *http.Request
	r := NewCoverArtArchiveResolver(capturingClient(&got))
	if _, err := r.Resolve(context.Background(), domain.ResultKindAlbum, "X", "Y", hostileMBID); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got == nil {
		t.Fatal("no request was made")
	}
	if got.URL.Host != "coverartarchive.org" {
		t.Errorf("host = %q, want coverartarchive.org", got.URL.Host)
	}
	if want := "/release-group/a%2Fb%3Fc%23d/front-1200"; got.URL.EscapedPath() != want {
		t.Errorf("escaped path = %q, want %q", got.URL.EscapedPath(), want)
	}
	if got.URL.RawQuery != "" || got.URL.Fragment != "" {
		t.Errorf("query = %q fragment = %q, want both empty; the mbid leaked out of its path segment",
			got.URL.RawQuery, got.URL.Fragment)
	}
}

func TestFanartTvArtworkResolver_EscapesMBIDInRequestURL(t *testing.T) {
	cases := []struct {
		name     string
		kind     domain.ResultKind
		wantPath string
	}{
		{"artist", domain.ResultKindArtist, "/v3/music/a%2Fb%3Fc%23d"},
		{"album", domain.ResultKindAlbum, "/v3/music/albums/a%2Fb%3Fc%23d"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got *http.Request
			r := NewFanartTvArtworkResolver(capturingClient(&got), "key-1")
			if _, err := r.Resolve(context.Background(), tc.kind, "X", "Y", hostileMBID); err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got == nil {
				t.Fatal("no request was made")
			}
			if got.URL.Host != "webservice.fanart.tv" {
				t.Errorf("host = %q, want webservice.fanart.tv", got.URL.Host)
			}
			if got.URL.EscapedPath() != tc.wantPath {
				t.Errorf("escaped path = %q, want %q", got.URL.EscapedPath(), tc.wantPath)
			}
			if got.URL.RawQuery != "api_key=key-1" || got.URL.Fragment != "" {
				t.Errorf("query = %q fragment = %q, want api_key=key-1 and no fragment",
					got.URL.RawQuery, got.URL.Fragment)
			}
		})
	}
}
