package goapi_test

import (
	"altune/overseer/internal/goapi"
	"errors"
	"testing"
)

func TestBaseURLFromEnv(t *testing.T) {
	cases := []struct {
		name, env, want string
		ok              bool
	}{
		{"empty", "", "", false},
		{"blank", "  \t", "", false},
		{"trimmed", " http://x:1 ", "http://x:1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("OVERSEER_GOAPI_URL", c.env)
			got, ok := goapi.BaseURLFromEnv()
			if got != c.want || ok != c.ok {
				t.Fatalf("BaseURLFromEnv() = %q, %v; want %q, %v", got, ok, c.want, c.ok)
			}
		})
	}
}

func TestNullEventSourceReportsBucketError(t *testing.T) {
	down := errors.New("bucket down")
	n := goapi.NewNullEventSource(down)
	if n.Status() != goapi.StatusDown {
		t.Fatalf("status = %v, want down", n.Status())
	}
	if !errors.Is(n.LastError(), down) {
		t.Fatalf("LastError() = %v, want wrapping %v", n.LastError(), down)
	}
}
