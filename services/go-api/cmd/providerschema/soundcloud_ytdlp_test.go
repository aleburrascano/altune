package main

import (
	"context"
	"errors"
	"slices"
	"testing"
)

func ytdlpSearch(run ytdlpRunner) fetcher {
	return newSoundCloudSource(nil, "", "", run)["ytdlp_search"]
}

func TestSoundCloudYtdlpRunsTheAdapterArgsAndReturnsTheTopHit(t *testing.T) {
	var args []string
	run := func(_ context.Context, a []string) ([]byte, error) {
		args = a
		return []byte("{\"id\":\"1\"}\n{\"id\":\"2\"}\n"), nil
	}
	raw, err := ytdlpSearch(run)(context.Background(), "nia archives")
	if err != nil || string(raw) != `{"id":"1"}` {
		t.Fatalf("got %s, %v", raw, err)
	}
	if !slices.Equal(args, []string{"--dump-json", "--flat-playlist", "--no-download", "scsearch5:nia archives"}) {
		t.Fatalf("got args %q", args)
	}
}

func TestSoundCloudYtdlpFailsWithNoResults(t *testing.T) {
	run := func(context.Context, []string) ([]byte, error) { return []byte("\n"), nil }
	if _, err := ytdlpSearch(run)(context.Background(), "nothing"); err == nil {
		t.Fatal("want an error")
	}
}

func TestSoundCloudYtdlpFailsWhenYtdlpFails(t *testing.T) {
	run := func(context.Context, []string) ([]byte, error) { return nil, errors.New("exit status 1: ERROR") }
	if _, err := ytdlpSearch(run)(context.Background(), "q"); err == nil {
		t.Fatal("want an error")
	}
}

func TestSoundCloudYtdlpSourceRoutesTheEntity(t *testing.T) {
	run := func(context.Context, []string) ([]byte, error) { return []byte(`{"id":"3"}`), nil }
	raw, err := newSoundCloudSource(nil, "", "", run).Resource(context.Background(), "ytdlp_search", "q")
	if err != nil || string(raw) != `{"id":"3"}` {
		t.Fatalf("got %s, %v", raw, err)
	}
}
