package app

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"
)

func writeFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	body := `{"label":"x","exchanges":[{"method":"GET","url":"http://example.test/search?q=a","status":200,"response_body":"{\"ok\":true}"}]}`
	if err := os.WriteFile(filepath.Join(dir, "corpus.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProviderTransport_ProdNeverBuildsReplayer(t *testing.T) {
	dir := writeFixture(t)
	cases := []config.Config{
		{Env: "production", ProviderReplayOptIn: true, ProviderReplayDir: dir},
		{Env: "", ProviderReplayOptIn: true, ProviderReplayDir: dir},
		{Env: "test", ProviderReplayOptIn: false, ProviderReplayDir: dir},
		{Env: "test", ProviderReplayOptIn: true},
	}
	for _, c := range cases {
		rt, err := providerTransport(&c)
		if err != nil || rt != nil {
			t.Fatalf("cfg %+v: got transport %v err %v, want nil", c, rt, err)
		}
	}
}

func TestProviderTransport_ReplaysStickyAndErrorsOnMiss(t *testing.T) {
	cfg := &config.Config{Env: "test", ProviderReplayOptIn: true, ProviderReplayDir: writeFixture(t)}
	rt, err := providerTransport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rt.(*httptrace.Replayer); !ok {
		t.Fatalf("got %T, want *httptrace.Replayer", rt)
	}
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodGet, "http://example.test/search?q=a", nil)
		resp, err := rt.RoundTrip(req)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		_ = resp.Body.Close()
	}
	miss, _ := http.NewRequest(http.MethodGet, "http://example.test/other", nil)
	if _, err := rt.RoundTrip(miss); err == nil {
		t.Fatal("unmatched request must error")
	}
}
