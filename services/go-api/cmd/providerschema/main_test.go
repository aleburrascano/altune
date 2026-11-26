package main

import (
	"altune/go-api/internal/shared/providerfields"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeSource struct {
	reference providerfields.Observed
	resources map[string]string
}

func (f fakeSource) Reference(context.Context, string) (providerfields.Observed, error) {
	return f.reference, nil
}

func (f fakeSource) Resource(_ context.Context, _, id string) (json.RawMessage, error) {
	raw, ok := f.resources[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return json.RawMessage(raw), nil
}

func noReads(providerfields.Schema) (map[string]providerfields.Reads, error) {
	return map[string]providerfields.Reads{}, nil
}

func writeSchema(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const seed = "provider: p\nreference: r\nentities:\n  song:\n    fixtures:\n      - {id: \"1\", why: plain}\n      - {id: \"2\", why: no isrc}\n    fields: []\n"

func TestRefreshMergesTheReferenceWithEveryFixture(t *testing.T) {
	path := writeSchema(t, seed)
	src := fakeSource{
		reference: providerfields.Observed{"id": "string", "attributes.isrc": "string"},
		resources: map[string]string{"1": `{"id":"1","attributes":{"isrc":"X"}}`, "2": `{"id":"2","attributes":{"audioLocale":"zxx"}}`},
	}
	var out bytes.Buffer
	if err := refresh(context.Background(), src, path, noReads, &out); err != nil {
		t.Fatal(err)
	}
	s, err := providerfields.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	song := s.Entities["song"]
	if song == nil {
		t.Fatal("the refreshed schema lost its song entity")
	}
	var got []string
	for _, f := range song.Fields {
		got = append(got, f.Path+"="+strings.Join(f.Seen, "+"))
	}
	want := "attributes=live attributes.audioLocale=live attributes.isrc=reference+live id=reference+live"
	if strings.Join(got, " ") != want {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(out.String(), "song: 4 fields, 0 only in the reference, 2 only live, 0 decoded but never sent, 0 read only by key match, 4 with no read flag or reason") {
		t.Fatalf("report: %s", out.String())
	}
}

func TestRefreshFailsLoudlyAndWritesNothingWhenAFixtureFails(t *testing.T) {
	path := writeSchema(t, seed)
	src := fakeSource{resources: map[string]string{"1": `{"id":"1"}`}}
	err := refresh(context.Background(), src, path, noReads, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "song fixture 2") {
		t.Fatalf("want an error naming the fixture, got %v", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != seed {
		t.Fatal("the schema was rewritten after a failure")
	}
}

func TestRefreshFailsOnAMissingSchema(t *testing.T) {
	err := refresh(context.Background(), fakeSource{}, filepath.Join(t.TempDir(), "none.yaml"), noReads, &bytes.Buffer{})
	if err == nil {
		t.Fatal("want an error")
	}
}

func TestRefreshWritesNothingWhenMarkingReadFieldsFails(t *testing.T) {
	path := writeSchema(t, seed)
	src := fakeSource{resources: map[string]string{"1": `{"id":"1"}`, "2": `{"id":"2"}`}}
	fail := func(providerfields.Schema) (map[string]providerfields.Reads, error) {
		return nil, errors.New("no source")
	}
	if err := refresh(context.Background(), src, path, fail, &bytes.Buffer{}); err == nil {
		t.Fatal("want an error")
	}
	if raw, _ := os.ReadFile(path); string(raw) != seed {
		t.Fatal("the schema was rewritten after a failure")
	}
}

func TestReportCountsWhereEachFieldWasSeenAndWhatLacksAReason(t *testing.T) {
	ref, live := providerfields.SeenReference, providerfields.SeenLive
	entity := &providerfields.Entity{Fields: []providerfields.Field{
		{Path: "a", Seen: []string{ref}, Unused: "x"},
		{Path: "b", Seen: []string{live}, Read: true},
		{Path: "c", Seen: []string{ref, live}},
		{Path: "d", Read: true},
		{Path: "e", Read: true, Absent: "only the lookup sends it"},
		{Path: "f", Seen: []string{live}, Read: true, ByKey: true},
	}}
	var out bytes.Buffer
	report(&out, "track", entity)
	want := "track: 6 fields, 1 only in the reference, 2 only live, 2 decoded but never sent, 1 read only by key match, 2 with no read flag or reason\n"
	if out.String() != want {
		t.Fatalf("got %q", out.String())
	}
}
