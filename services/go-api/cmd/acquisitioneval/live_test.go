package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type liveServer struct {
	failOEmbed bool
}

func (s liveServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/youtubei/v1/next":
			var body struct {
				VideoID string `json:"videoId"`
			}
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &body)
			_, _ = w.Write(readTestdata(t, "next_"+body.VideoID+".json"))
		case "/youtubei/v1/search":
			_, _ = w.Write(readTestdata(t, "search_lucki_2021_vibes.json"))
		case "/oembed":
			if s.failOEmbed {
				http.Error(w, "gone", http.StatusNotFound)
				return
			}
			id := strings.TrimPrefix(r.URL.Query().Get("url"), "https://www.youtube.com/watch?v=")
			_, _ = w.Write(readTestdata(t, "oembed_"+id+".json"))
		default:
			http.NotFound(w, r)
		}
	})
}

func (s liveServer) client(t *testing.T) *ytmsearch.Client {
	srv := httptest.NewServer(s.handler(t))
	t.Cleanup(srv.Close)
	return ytmsearch.New(ytmsearch.Config{MusicURL: srv.URL, OEmbedURL: srv.URL + "/oembed", Timeout: 5 * time.Second})
}

const liveTracksJSONL = `{"id":"t-atv","title":"remember them days","artist":"SLAYR","album":"a","duration_seconds":113,"audio_source_url":"https://www.youtube.com/watch?v=XDC3uH1FSgw","acquisition_provenance":"verified","acquisition_evidence":{"duration_delta_seconds":0.4},"isrc":"ignored"}

{"id":"t-omv","title":"remember them days","artist":"SLAYR","album":"a","duration_seconds":113,"audio_source_url":"https://music.youtube.com/watch?v=u4GAeAYGAqE","acquisition_provenance":"corroborated","acquisition_evidence":{"duration_delta_seconds":0.48}}
{"id":"t-clean","title":"2021 Vibes","artist":"LUCKI","album":"a","duration_seconds":125,"audio_source_url":"https://music.youtube.com/watch?v=88oFlVnMEsc","acquisition_provenance":"verified","acquisition_evidence":null}
{"id":"t-sc","title":"REEFER","artist":"rexv2","album":"a","duration_seconds":105,"audio_source_url":"https://soundcloud.com/the-yadcris/rexv2-reefer-instrumental","acquisition_provenance":"verified","acquisition_evidence":null}
`

func writeTracks(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tracks.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func readJudged(t *testing.T, path string) map[string]judgedRow {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows := map[string]judgedRow{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var row judgedRow
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("bad output line %q: %v", line, err)
		}
		rows[row.ID] = row
	}
	return rows
}

func TestRunLive_JudgesEveryTrackAndWritesRowsAndSummary(t *testing.T) {
	tracks := writeTracks(t, liveTracksJSONL)
	out := filepath.Join(t.TempDir(), "judged.jsonl")
	var stdout bytes.Buffer

	code := runLive([]string{"-tracks", tracks, "-out", out}, &stdout, liveServer{}.client(t))

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	rows := readJudged(t, out)
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want one per track", len(rows))
	}
	omv := rows["t-omv"]
	if got := strings.Join(omv.Flags, ","); got != "music_video(OMV),video_title" {
		t.Errorf("omv flags = %q", got)
	}
	if omv.VideoType != "MUSIC_VIDEO_TYPE_OMV" || omv.Author != "slayr" || omv.SourceTitle != "slayr - remember them days (OFFICIAL MUSIC VIDEO)" {
		t.Errorf("omv row = %+v", omv)
	}
	if omv.URL != "https://music.youtube.com/watch?v=u4GAeAYGAqE" || omv.Title != "remember them days" || omv.Artist != "SLAYR" {
		t.Errorf("omv identity fields = %+v", omv)
	}
	if got := rows["t-atv"].Flags; len(got) != 0 {
		t.Errorf("atv flags = %v, want none", got)
	}
	if got := strings.Join(rows["t-clean"].Flags, ","); got != "clean_edit(explicit exists)" {
		t.Errorf("clean flags = %q", got)
	}
	sc := rows["t-sc"]
	if len(sc.Flags) != 0 || sc.Marker != "skipped:soundcloud.com" {
		t.Errorf("soundcloud row = %+v, want no flags and a skipped marker", sc)
	}
	summary := stdout.String()
	for _, want := range []string{"music_video(OMV)", "video_title", "clean_edit(explicit exists)", "skipped:soundcloud.com", "flagged 2 of 4"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q:\n%s", want, summary)
		}
	}
}

func TestRunLive_FailedLookupDegradesToALookupFailedFlag(t *testing.T) {
	tracks := writeTracks(t, liveTracksJSONL)
	out := filepath.Join(t.TempDir(), "judged.jsonl")
	var stdout bytes.Buffer

	code := runLive([]string{"-tracks", tracks, "-out", out}, &stdout, liveServer{failOEmbed: true}.client(t))

	if code != 0 {
		t.Fatalf("exit code = %d, a failed lookup must not abort the run", code)
	}
	rows := readJudged(t, out)
	omv := rows["t-omv"]
	if got := strings.Join(omv.Flags, ","); got != "music_video(OMV),lookup_failed:oembed" {
		t.Errorf("omv flags = %q, want the YTM verdict kept and the oembed step marked failed", got)
	}
	if omv.SourceTitle != "remember them days" {
		t.Errorf("source title = %q, want the YTM title as the fallback", omv.SourceTitle)
	}
	if len(rows) != 4 {
		t.Errorf("rows = %d, want every track still judged", len(rows))
	}
}

func TestRunLive_UnreachableYouTubeMarksEveryStepFailed(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	client := ytmsearch.New(ytmsearch.Config{MusicURL: srv.URL, OEmbedURL: srv.URL + "/oembed", Timeout: time.Second})
	tracks := writeTracks(t, strings.Split(liveTracksJSONL, "\n")[0]+"\n")
	out := filepath.Join(t.TempDir(), "judged.jsonl")

	code := runLive([]string{"-tracks", tracks, "-out", out}, io.Discard, client)

	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := strings.Join(readJudged(t, out)["t-atv"].Flags, ",")
	if got != "lookup_failed:next,lookup_failed:oembed,lookup_failed:search" {
		t.Fatalf("flags = %q", got)
	}
}

func TestRunLive_BadInputExitsNonZero(t *testing.T) {
	out := filepath.Join(t.TempDir(), "judged.jsonl")
	tests := map[string][]string{
		"no tracks flag":      {"-out", out},
		"no out flag":         {"-tracks", writeTracks(t, liveTracksJSONL)},
		"malformed line":      {"-tracks", writeTracks(t, "{not json}\n"), "-out", out},
		"missing tracks file": {"-tracks", filepath.Join(t.TempDir(), "absent.jsonl"), "-out", out},
	}

	for name, args := range tests {
		t.Run(name, func(t *testing.T) {
			if code := runLive(args, io.Discard, liveServer{}.client(t)); code != 2 {
				t.Fatalf("exit code = %d, want 2", code)
			}
		})
	}
}

func TestYoutubeVideoID(t *testing.T) {
	tests := map[string]struct {
		url    string
		wantID string
		wantOK bool
	}{
		"watch url":         {url: "https://www.youtube.com/watch?v=XDC3uH1FSgw", wantID: "XDC3uH1FSgw", wantOK: true},
		"music watch url":   {url: "https://music.youtube.com/watch?v=u4GAeAYGAqE&list=RD", wantID: "u4GAeAYGAqE", wantOK: true},
		"short url":         {url: "https://youtu.be/XDC3uH1FSgw", wantID: "XDC3uH1FSgw", wantOK: true},
		"not an 11 char id": {url: "https://www.youtube.com/watch?v=short", wantOK: false},
		"soundcloud":        {url: "https://soundcloud.com/a/b", wantOK: false},
		"empty":             {url: "", wantOK: false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			id, ok := youtubeVideoID(tc.url)
			if id != tc.wantID || ok != tc.wantOK {
				t.Fatalf("youtubeVideoID(%q) = %q, %v; want %q, %v", tc.url, id, ok, tc.wantID, tc.wantOK)
			}
		})
	}
}

func TestLiveConfigFromEnv(t *testing.T) {
	env := map[string]string{"ACQEVAL_LIVE_INTERVAL": "250ms", "ACQEVAL_LIVE_TIMEOUT": "7s"}
	cfg, err := liveConfigFromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinInterval != 250*time.Millisecond || cfg.Timeout != 7*time.Second {
		t.Errorf("cfg = %+v", cfg)
	}

	defaults, err := liveConfigFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if defaults.MinInterval != time.Second || defaults.Timeout != 20*time.Second {
		t.Errorf("defaults = %+v, want 1s interval and 20s timeout", defaults)
	}

	if _, err := liveConfigFromEnv(func(string) string { return "soon" }); err == nil {
		t.Error("want an error for an unparseable duration")
	}
}
