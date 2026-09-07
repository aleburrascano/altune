package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

type stubSource struct {
	name       string
	found      []ports.AudioCandidate
	findErr    error
	panicValue any
	fetchPath  string
	fetchErr   error
	fetched    []ports.AudioCandidate
}

func (s *stubSource) Name() string { return s.name }

func (s *stubSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	if s.panicValue != nil {
		panic(s.panicValue)
	}
	return s.found, s.findErr
}

func (s *stubSource) Fetch(_ context.Context, candidate ports.AudioCandidate, _ string) (string, error) {
	s.fetched = append(s.fetched, candidate)
	return s.fetchPath, s.fetchErr
}

func candidate(url string) ports.AudioCandidate {
	return ports.AudioCandidate{Title: "Blinding Lights", URL: url}
}

func TestSourceRegistry_NewFiltersNilSources(t *testing.T) {
	reg := NewSourceRegistry(nil, &stubSource{name: "a"}, nil)
	if got := reg.Names(); len(got) != 1 || got[0] != "a" {
		t.Fatalf("Names() = %v, want [a] with nil sources dropped", got)
	}
}

func TestSourceRegistry_FindNoSourcesConfigured(t *testing.T) {
	reg := NewSourceRegistry()
	_, err := reg.Find(context.Background(), ports.FindRequest{})
	if err == nil || !strings.Contains(err.Error(), "no audio sources configured") {
		t.Fatalf("err = %v, want no audio sources configured", err)
	}
}

func TestSourceRegistry_FindMergesStampsAndDedupesByURL(t *testing.T) {
	a := &stubSource{name: "a", found: []ports.AudioCandidate{candidate("u1"), candidate("shared")}}
	b := &stubSource{name: "b", found: []ports.AudioCandidate{candidate("shared"), candidate("u2"), candidate("")}}
	reg := NewSourceRegistry(a, b)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	wantURLs := []string{"u1", "shared", "u2"}
	if len(got) != len(wantURLs) {
		t.Fatalf("got %d candidates %v, want %v", len(got), got, wantURLs)
	}
	for i, c := range got {
		if c.URL != wantURLs[i] {
			t.Errorf("candidate %d URL = %q, want %q", i, c.URL, wantURLs[i])
		}
	}
	if got[0].Source != "a" || got[1].Source != "a" || got[2].Source != "b" {
		t.Errorf("sources = %q/%q/%q, want a/a/b — first source wins a duplicate URL", got[0].Source, got[1].Source, got[2].Source)
	}
}

func TestSourceRegistry_FindSurvivesPanickingSource(t *testing.T) {
	panicky := &stubSource{name: "panicky", panicValue: "boom"}
	healthy := &stubSource{name: "healthy", found: []ports.AudioCandidate{candidate("u1")}}
	reg := NewSourceRegistry(panicky, healthy)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("one panicking source must not fail the search, got err: %v", err)
	}
	if len(got) != 1 || got[0].URL != "u1" || got[0].Source != "healthy" {
		t.Fatalf("got %v, want the healthy source's single candidate", got)
	}
}

func TestSourceRegistry_FindReturnsPartialOnSingleFailure(t *testing.T) {
	failing := &stubSource{name: "failing", findErr: errors.New("down")}
	healthy := &stubSource{name: "healthy", found: []ports.AudioCandidate{candidate("u1")}}
	reg := NewSourceRegistry(failing, healthy)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("one failing source must not fail the search, got err: %v", err)
	}
	if len(got) != 1 || got[0].URL != "u1" {
		t.Fatalf("got %v, want only the healthy source's candidate", got)
	}
}

func TestSourceRegistry_FindFailsWhenEverySourceFails(t *testing.T) {
	first := &stubSource{name: "first", findErr: errors.New("first down")}
	second := &stubSource{name: "second", panicValue: "second boom"}
	reg := NewSourceRegistry(first, second)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if got != nil {
		t.Fatalf("got %v, want nil when every source failed", got)
	}
	if err == nil || !strings.Contains(err.Error(), "every audio source failed") {
		t.Fatalf("err = %v, want it to report every audio source failed", err)
	}
	if !strings.Contains(err.Error(), "first down") {
		t.Errorf("err = %v, want it to wrap the first failure", err)
	}
}

func TestMergeSlots_DedupesFailsAllAndKeepsFirstError(t *testing.T) {
	sources := []ports.AudioSource{
		&stubSource{name: "a"},
		&stubSource{name: "b"},
		&stubSource{name: "c"},
	}
	slots := [][]ports.AudioCandidate{
		{candidate("u1"), candidate("dup")},
		nil,
		{candidate("dup"), candidate("u2")},
	}
	errs := []error{nil, errors.New("b failed"), nil}

	got, err := mergeSlots(context.Background(), sources, slots, errs)
	if err != nil {
		t.Fatalf("mergeSlots returned error with a surviving source: %v", err)
	}
	wantURLs := []string{"u1", "dup", "u2"}
	if len(got) != len(wantURLs) {
		t.Fatalf("got %v, want %v", got, wantURLs)
	}
	for i, c := range got {
		if c.URL != wantURLs[i] {
			t.Errorf("candidate %d URL = %q, want %q", i, c.URL, wantURLs[i])
		}
	}

	allErrs := []error{errors.New("a failed"), errors.New("b failed"), errors.New("c failed")}
	_, err = mergeSlots(context.Background(), sources, [][]ports.AudioCandidate{nil, nil, nil}, allErrs)
	if err == nil || !strings.Contains(err.Error(), "a failed") {
		t.Fatalf("err = %v, want every-source-failed wrapping the first error", err)
	}
}

func TestSourceRegistry_FetchRoutesToOwningSource(t *testing.T) {
	a := &stubSource{name: "a"}
	b := &stubSource{name: "b", fetchPath: "/tmp/b.mp3"}
	reg := NewSourceRegistry(a, b)

	path, err := reg.Fetch(context.Background(), ports.AudioCandidate{URL: "u2", Source: "b"}, "/out")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if path != "/tmp/b.mp3" {
		t.Errorf("path = %q, want the owning source's result", path)
	}
	if len(a.fetched) != 0 {
		t.Errorf("source a received %d fetches, want 0", len(a.fetched))
	}
	if len(b.fetched) != 1 || b.fetched[0].URL != "u2" {
		t.Errorf("source b fetched = %v, want the u2 candidate", b.fetched)
	}
}

func TestSourceRegistry_FetchUnknownSource(t *testing.T) {
	reg := NewSourceRegistry(&stubSource{name: "a"})

	_, err := reg.Fetch(context.Background(), ports.AudioCandidate{URL: "u1", Source: "ghost"}, "/out")
	if err == nil {
		t.Fatal("expected an error routing to an unknown source")
	}
	if !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "u1") {
		t.Errorf("err = %v, want it to name the missing source and candidate", err)
	}
}
