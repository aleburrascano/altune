package main

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/httptrace"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"
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
		{Method: "GET", URL: js, Status: 200, RespBody: "bigjs"},
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

type deezerOnlyTransport struct{}

func (deezerOnlyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	status, body := http.StatusNotFound, `{}`
	if req.URL.Host == "api.deezer.com" && req.URL.Path == "/search/track" {
		status, body = http.StatusOK, `{"data":[{"id":1,"title":"Probe Song","artist":{"id":2,"name":"Probe Artist"},"album":{"id":3,"title":"Probe Album"}}]}`
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

func titles(t *testing.T, searcher searchAdapter) []string {
	t.Helper()
	results, err := searcher.Search(t.Context(), "probe song")
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	var out []string
	for _, r := range results {
		out = append(out, r.Title)
	}
	return out
}

func TestFixtures_ARecordedSearchReplaysTheSameAnswersOffline(t *testing.T) {
	cfg := &config.Config{}
	rec := httptrace.NewRecorder(deezerOnlyTransport{})
	live, err := rankingOnlySearchService(t.Context(), cfg, nil, rec)
	if err != nil {
		t.Fatalf("ranking-only search: %v", err)
	}
	recorded := titles(t, searchAdapter{svc: live})
	live.WaitForBackground()
	if !slices.Contains(recorded, "Probe Song") {
		t.Fatalf("recorded search answered %v, want the Deezer track through the recorder", recorded)
	}
	dir := t.TempDir()
	if err := saveExchanges(dir, "corpus", dedupExchanges(rec.Exchanges())); err != nil {
		t.Fatalf("save: %v", err)
	}

	replayed, err := buildReplaySearcher(t.Context(), cfg, nil, dir)
	if err != nil {
		t.Fatalf("replay searcher: %v", err)
	}
	if got := titles(t, replayed); !slices.Equal(got, recorded) {
		t.Errorf("replayed search answered %v, want the recorded %v", got, recorded)
	}
}
