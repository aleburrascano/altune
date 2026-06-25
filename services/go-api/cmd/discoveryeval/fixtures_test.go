package main

import (
	"testing"

	"altune/go-api/internal/shared/httptrace"
)

func TestFixtures_SaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	ex := []httptrace.Exchange{
		{Method: "GET", URL: "https://api.deezer.com/search?q=foo", Status: 200, RespBody: `{"a":1}`},
		{Method: "GET", URL: "https://musicbrainz.org/ws/2/?query=foo", Status: 200, RespBody: `{"b":2}`},
		{Method: "POST", URL: "https://music.youtube.com/youtubei/v1/search", Status: 200, ReqBody: `{"query":"bar"}`, RespBody: `{"c":3}`},
	}
	if err := saveExchanges(dir, "corpus", ex); err != nil {
		t.Fatalf("save: %v", err)
	}

	all, err := loadAllFixtures(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("loaded %d exchanges, want 3", len(all))
	}

	rep := httptrace.NewReplayer(all)
	if rep.Remaining() != 3 {
		t.Errorf("replayer has %d remaining, want 3", rep.Remaining())
	}
}

func TestDedupExchanges_CollapsesIdenticalRequests(t *testing.T) {
	js := "https://a-v2.sndcdn.com/assets/55-fbdbfe63.js"
	ex := []httptrace.Exchange{
		{Method: "GET", URL: js, Status: 200, RespBody: "bigjs"},
		{Method: "GET", URL: js, Status: 200, RespBody: "bigjs"}, // racing duplicate bootstrap
		{Method: "GET", URL: "https://api.deezer.com/search?q=a", Status: 200, RespBody: "a"},
		{Method: "GET", URL: "https://api.deezer.com/search?q=b", Status: 200, RespBody: "b"},
	}
	out := dedupExchanges(ex)
	if len(out) != 3 {
		t.Fatalf("deduped to %d, want 3 (the duplicate JS bootstrap collapsed)", len(out))
	}
}

func TestDedupExchanges_DistinctBodiesKept(t *testing.T) {
	url := "https://music.youtube.com/youtubei/v1/search"
	ex := []httptrace.Exchange{
		{Method: "POST", URL: url, ReqBody: `{"query":"a"}`, Status: 200, RespBody: "ra"},
		{Method: "POST", URL: url, ReqBody: `{"query":"b"}`, Status: 200, RespBody: "rb"},
	}
	if out := dedupExchanges(ex); len(out) != 2 {
		t.Fatalf("deduped to %d, want 2 (same URL, distinct bodies are distinct requests)", len(out))
	}
}

func TestLoadAllFixtures_MissingDirErrors(t *testing.T) {
	if _, err := loadAllFixtures(t.TempDir() + "/does-not-exist"); err == nil {
		t.Error("expected an error loading a missing fixtures dir")
	}
}
