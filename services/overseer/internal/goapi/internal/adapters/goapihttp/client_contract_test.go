package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStreamPathCannotChangeHost(t *testing.T) {
	var evilHits atomic.Int64
	evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		evilHits.Add(1)
	}))
	defer evil.Close()

	var goodHits atomic.Int64
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodHits.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer good.Close()

	hostilePaths := []string{
		"@" + evil.Listener.Addr().String(),
		"//" + evil.Listener.Addr().String() + "/x",
		evil.URL,
	}
	builds := map[string]func(path string) (streamConsumer, error){
		"events": func(path string) (streamConsumer, error) {
			return goapihttp.NewConsumer(good.URL, goapi.StaticTokenSource("op-token"),
				goapihttp.WithStreamPath(path), goapihttp.WithBackoff(fixedBackoff(time.Hour)))
		},
		"logs": func(path string) (streamConsumer, error) {
			return goapihttp.NewLogsConsumer(good.URL, goapi.StaticTokenSource("op-token"),
				goapihttp.WithLogsStreamPath(path), goapihttp.WithLogsBackoff(fixedBackoff(time.Hour)))
		},
	}
	for name, build := range builds {
		for _, p := range hostilePaths {
			c, err := build(p)
			if err != nil {
				t.Fatalf("%s: build consumer: %v", name, err)
			}
			stop := startConsumer(c)
			waitUntil(t, func() bool { return c.LastError() != nil }, name+" to dial "+p)
			stop()
		}
	}

	if evilHits.Load() != 0 {
		t.Fatal("a request reached the evil host: path changed the request host")
	}
	if goodHits.Load() != int64(len(builds)*len(hostilePaths)) {
		t.Fatalf("configured host saw %d requests, want one per hostile path", goodHits.Load())
	}
}

func TestClientDoesNotFollowRedirectLeakingBearer(t *testing.T) {
	var (
		attackerHits atomic.Int64
		amu          sync.Mutex
		gotAuth      []string
	)
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attackerHits.Add(1)
		amu.Lock()
		gotAuth = append(gotAuth, r.Header.Get("Authorization"))
		amu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	t.Cleanup(attacker.Close)

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL, http.StatusFound)
	}))
	t.Cleanup(api.Close)

	client, err := goapihttp.New(api.URL, goapi.StaticTokenSource("operator-bearer-SECRET"))
	if err != nil {
		t.Fatalf("New client: %v", err)
	}
	if _, err := client.Health(context.Background()); err == nil {
		t.Fatal("a redirect from go-api must not be followed into a success")
	}
	if got := attackerHits.Load(); got != 0 {
		amu.Lock()
		defer amu.Unlock()
		t.Fatalf("client followed the redirect: attacker got %d requests; auth=%v", got, gotAuth)
	}
}
