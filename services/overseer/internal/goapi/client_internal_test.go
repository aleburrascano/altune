package goapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestPathCannotChangeHost is the confused-deputy / token-leak guard: a path
// that tries to inject a host (userinfo, protocol-relative, absolute URL) must
// stay on the configured base host, so the operator bearer can never be sent
// elsewhere. get is unexported, so this white-box test drives it directly.
func TestPathCannotChangeHost(t *testing.T) {
	var evilHit bool
	evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		evilHit = true
	}))
	defer evil.Close()

	var goodHits int
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits++
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer good.Close()

	c, err := New(good.URL, StaticTokenSource("op-token"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hostiledPaths := []string{
		"@" + evil.Listener.Addr().String(),
		"//" + evil.Listener.Addr().String() + "/x",
		evil.URL, // a fully absolute URL as the "path"
	}
	for _, p := range hostiledPaths {
		var out Health
		// Errors are fine (the joined path likely 404s on the good host); what
		// matters is the request never reaches the evil host.
		_ = c.get(context.Background(), p, &out)
	}

	if evilHit {
		t.Fatal("a request reached the evil host: path changed the request host")
	}
	if goodHits == 0 {
		t.Fatal("no request reached the configured host; test did not exercise the path")
	}
}
