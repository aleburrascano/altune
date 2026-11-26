package chromaprint_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/adapters/chromaprint"
	"altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/retrysafe"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func newIdentifier(t *testing.T, binDir, apiKey string) *chromaprint.Identifier {
	t.Helper()
	id, err := chromaprint.NewIdentifier(context.Background(), binDir, apiKey, &http.Client{})
	if err != nil {
		t.Fatalf("NewChromaprintIdentifier: %v", err)
	}
	return id
}

func TestNewIdentifier_RefusesANilClient(t *testing.T) {
	id, err := chromaprint.NewIdentifier(context.Background(), t.TempDir(), "key", nil)

	if !errors.Is(err, chromaprint.ErrClientRequired) {
		t.Fatalf("err = %v, want ErrChromaprintClientRequired", err)
	}
	if id != nil {
		t.Errorf("identifier = %v, want nil beside the error", id)
	}
}

func TestNewIdentifier_SendsLookupsThroughTheClientItWasGiven(t *testing.T) {
	srv := lookupServerServing(`{"status":"ok","results":[]}`)
	defer srv.Close()
	var through int
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		through++
		return http.DefaultTransport.RoundTrip(r)
	})}
	id, err := chromaprint.NewIdentifier(context.Background(), fakeFpcalcDir(t, 100), "real-key", client)
	if err != nil {
		t.Fatalf("NewChromaprintIdentifier: %v", err)
	}

	if _, err := id.WithEndpoint(srv.URL).Identify(context.Background(), "/tmp/full.mp3", 0); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if through != 1 {
		t.Errorf("requests through the supplied client = %d, want 1", through)
	}
}

func TestIdentify_MarksTheLookupPostRetrySafeSoTheTransportMayRetryIt(t *testing.T) {
	srv := lookupServerServing(`{"status":"ok","results":[]}`)
	defer srv.Close()
	var marked bool
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		marked = retrysafe.Marked(r.Context())
		return http.DefaultTransport.RoundTrip(r)
	})}
	id, err := chromaprint.NewIdentifier(context.Background(), fakeFpcalcDir(t, 100), "real-key", client)
	if err != nil {
		t.Fatalf("NewChromaprintIdentifier: %v", err)
	}

	if _, err := id.WithEndpoint(srv.URL).Identify(context.Background(), "/tmp/full.mp3", 0); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if !marked {
		t.Error("the lookup POST was not marked retry-safe, so the transport would never retry a throttled answer")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func matchFromBody(t *testing.T, body string) download.RecordingMatch {
	t.Helper()
	srv := lookupServerServing(body)
	t.Cleanup(srv.Close)
	match, err := newIdentifier(t, fakeFpcalcDir(t, 100), "real-key").WithEndpoint(srv.URL).Identify(context.Background(), "/tmp/full.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}
	return match
}

func TestIdentify_PicksHighestScoreAboveThreshold(t *testing.T) {
	got := matchFromBody(t, `{"status":"ok","results":[
		{"id":"weak","score":0.9,"recordings":[{"id":"mbid-a"}]},
		{"id":"strong","score":0.98,"recordings":[{"id":"mbid-b"}]}]}`)

	if got.AcoustID != "strong" {
		t.Errorf("AcoustID = %q, want strong", got.AcoustID)
	}
	if !slices.Contains(got.MBIDs, "mbid-b") {
		t.Errorf("MBIDs = %v, want mbid-b", got.MBIDs)
	}
}

func TestIdentify_IgnoresLowScores(t *testing.T) {
	got := matchFromBody(t, `{"status":"ok","results":[{"id":"weak","score":0.3,"recordings":[{"id":"mbid-a"}]}]}`)
	if got.Known() {
		t.Errorf("a below-threshold result must not count as known, got %+v", got)
	}
}

func TestIdentify_KeepsResultsWithOnlyAnAcoustID(t *testing.T) {
	got := matchFromBody(t, `{"status":"ok","results":[{"id":"acoustid-only","score":0.99}]}`)
	if !got.Known() {
		t.Fatalf("an AcoustID with no MusicBrainz links still identifies the audio, got %+v", got)
	}
	if got.AcoustID != "acoustid-only" {
		t.Errorf("AcoustID = %q, want acoustid-only", got.AcoustID)
	}
}

type recordingTransport struct{ urls []string }

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.urls = append(r.urls, req.URL.String())
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"status":"ok","results":[]}`)),
		Request:    req,
	}, nil
}

func TestNewIdentifier_DefaultsToAcoustIDEndpoint(t *testing.T) {
	transport := &recordingTransport{}
	prev := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = prev })

	if _, err := newIdentifier(t, fakeFpcalcDir(t, 100), "key").Identify(context.Background(), "/tmp/full.mp3", 0); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if want := []string{"https://api.acoustid.org/v2/lookup"}; !slices.Equal(transport.urls, want) {
		t.Errorf("requests = %v, want %v", transport.urls, want)
	}
}

type warnCapture struct {
	warned bool
	msgs   []string
}

func (h *warnCapture) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelWarn
}

func (h *warnCapture) Handle(_ context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		h.warned = true
		h.msgs = append(h.msgs, r.Message)
	}
	return nil
}

func (h *warnCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *warnCapture) WithGroup(string) slog.Handler      { return h }

func captureWarnings(t *testing.T) *warnCapture {
	t.Helper()
	h := &warnCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func TestNewIdentifier_EmptyKeyWarnsAtStartup(t *testing.T) {
	logs := captureWarnings(t)

	newIdentifier(t, "", "")

	if !logs.warned {
		t.Fatal("an empty AcoustID API key must warn at startup, got no warning")
	}
}

func TestNewIdentifier_WhitespaceOnlyKeyWarnsAndDisables(t *testing.T) {
	logs := captureWarnings(t)

	id := newIdentifier(t, "", "   \t\n ")

	if !logs.warned {
		t.Fatal("a whitespace-only AcoustID API key must warn at startup, got no warning")
	}
	if id.Available() {
		t.Error("a whitespace-only key must not report the identifier as available")
	}
}

func TestNewIdentifier_ValidKeyDoesNotWarn(t *testing.T) {
	logs := captureWarnings(t)

	newIdentifier(t, "", "real-key")

	if logs.warned {
		t.Errorf("a valid API key must not warn, got %v", logs.msgs)
	}
}

func captureJSONLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestNewIdentifier_BlankKeyLogsExactlyOneWarn(t *testing.T) {
	logs := captureJSONLogs(t)

	newIdentifier(t, t.TempDir(), "   ")

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("blank key logged %d lines, want exactly 1:\n%s", len(lines), logs.String())
	}
	var record struct {
		Level string `json:"level"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &record); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	if record.Level != "WARN" {
		t.Errorf("level = %q, want WARN", record.Level)
	}
	if !strings.HasPrefix(record.Msg, "chromaprint: AcoustID API key is empty or blank") {
		t.Errorf("msg = %q, want the empty-or-blank AcoustID key warning", record.Msg)
	}
}

func TestNewIdentifier_NonBlankKeyLogsNothing(t *testing.T) {
	logs := captureJSONLogs(t)

	newIdentifier(t, t.TempDir(), "key")

	if logs.Len() != 0 {
		t.Errorf("a non-blank key logged:\n%s", logs.String())
	}
}

func TestLookup_EmptyKeyReturnsError(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDir(t, 100), "").WithEndpoint(srv.URL)

	_, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)

	if !errors.Is(err, chromaprint.ErrMissingAPIKey) {
		t.Fatalf("Identify with empty key err = %v, want ErrChromaprintMissingAPIKey", err)
	}
	if hits != 0 {
		t.Errorf("hits = %d, want 0 without a key", hits)
	}
}

func TestLookup_OversizedResponseBodyIsRejected(t *testing.T) {
	oversized := `{"status":"ok","results":[{"id":"` + strings.Repeat("x", acoustIDBodyCap+1024) + `"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oversized))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 100), "real-key").WithEndpoint(srv.URL)

	_, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)

	if err == nil {
		t.Fatal("lookup against an oversized response body must error, got nil")
	}
}

const emptyFingerprint = "empty"

var fakeFpcalcDirs = map[string]string{}

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "chromaprint-fpcalc-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create fake fpcalc root: %v\n", err)
		os.Exit(1)
	}
	code := 1
	if err := writeFakeFpcalcs(root); err != nil {
		fmt.Fprintf(os.Stderr, "write fake fpcalc: %v\n", err)
	} else {
		code = m.Run()
	}
	_ = os.RemoveAll(root)
	os.Exit(code)
}

func writeFakeFpcalcs(root string) error {
	outputs := map[string]string{emptyFingerprint: `{"duration": 0, "fingerprint": ""}`}
	for _, seconds := range []float64{100, 130, 200, 236} {
		outputs[fmt.Sprintf("%g", seconds)] = fmt.Sprintf(`{"duration": %g, "fingerprint": "AQAAdkmVJUqSpNce"}`, seconds)
	}
	for name, output := range outputs {
		dir := filepath.Join(root, name)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return fmt.Errorf("make the fake fpcalc dir: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "fpcalc"), []byte("#!/bin/sh\nprintf '"+output+"'\n"), 0o755); err != nil {
			return fmt.Errorf("write the fake fpcalc: %w", err)
		}
		fakeFpcalcDirs[name] = dir
	}
	return nil
}

func fakeFpcalcDir(t *testing.T, duration float64) string {
	t.Helper()
	dir, ok := fakeFpcalcDirs[fmt.Sprintf("%g", duration)]
	if !ok {
		t.Fatalf("no fake fpcalc reports %gs; add it to writeFakeFpcalcs", duration)
	}
	return dir
}

func TestIdentify_SendsCandidateDurationNotFpcalcsMeasuredLength(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 130), "real-key").WithEndpoint(srv.URL)

	if _, err := id.Identify(context.Background(), "/tmp/preview.mp3", 236); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if got := gotForm.Get("duration"); got != "236" {
		t.Errorf("duration = %q, want 236 (the candidate's full length)", got)
	}
	if got := gotForm.Get("meta"); got != "recordings releasegroups" {
		t.Errorf("meta = %q, want %q", got, "recordings releasegroups")
	}
}

func TestIdentify_ReturnsEveryResultAboveTheScoreFloorWithLinkedRecordings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[
			{"id":"ac-weak","score":0.5,"recordings":[{"id":"mb-weak","title":"Below Floor","duration":300}]},
			{"id":"ac-top","score":0.98,"recordings":[{"id":"mb-top","title":"Song One","duration":236,"artists":[{"name":"Artist A"}]}]},
			{"id":"ac-second","score":0.9,"recordings":[{"id":"mb-second","title":"Song Two","duration":220,"artists":[{"name":"Artist B"}]}]}
		]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 236), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if match.AcoustID != "ac-top" || match.Score != 0.98 || !slices.Contains(match.MBIDs, "mb-top") {
		t.Fatalf("top match = %+v, want the ac-top result as before", match)
	}
	if len(match.Results) != 2 {
		t.Fatalf("Results = %+v, want 2 results at or above the score floor", match.Results)
	}
	if match.Results[0].ID != "ac-top" || match.Results[1].ID != "ac-second" {
		t.Errorf("Results order = [%s %s], want [ac-top ac-second]", match.Results[0].ID, match.Results[1].ID)
	}
	got := match.Results[0].Recordings[0]
	want := ports.LinkedRecording{MBID: "mb-top", Title: "Song One", Artists: []string{"Artist A"}, Duration: 236}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("linked recording = %+v, want %+v", got, want)
	}
}

func TestIdentify_CarriesReleaseGroupsOnLinkedRecordings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","results":[{"id":"ac-top","score":0.98,"recordings":[
			{"id":"mb-live","title":"Song","releasegroups":[
				{"id":"rg-1","title":"Song Live","type":"Album","secondarytypes":["Live","Compilation"]},
				{"id":"rg-2","title":"Song","type":"Single"}]},
			{"id":"mb-bare","title":"Song"}]}]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 236), "real-key").WithEndpoint(srv.URL)
	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	recs := match.Results[0].Recordings
	want := []ports.ReleaseGroup{
		{MBID: "rg-1", Title: "Song Live", Type: "Album", SecondaryTypes: []string{"Live", "Compilation"}},
		{MBID: "rg-2", Title: "Song", Type: "Single"},
	}
	if !reflect.DeepEqual(recs[0].ReleaseGroups, want) {
		t.Errorf("release groups = %+v, want %+v", recs[0].ReleaseGroups, want)
	}
	if recs[1].ReleaseGroups != nil {
		t.Errorf("a recording without release groups must carry nil, got %+v", recs[1].ReleaseGroups)
	}
}

func TestIdentify_TheMatchIsTheTopResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","results":[
			{"id":"ac-first","score":0.95,"recordings":[{"id":"mb-first"}]},
			{"id":"ac-tied","score":0.95,"recordings":[{"id":"mb-tied"}]}]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 236), "real-key").WithEndpoint(srv.URL)
	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	top := match.Results[0]
	if top.ID != "ac-first" || match.AcoustID != top.ID || match.Score != top.Score ||
		!reflect.DeepEqual(match.MBIDs, []string{"mb-first"}) {
		t.Errorf("match = %+v, want it derived from Results[0] = %+v", match, top)
	}
}

func TestIdentify_ZeroFullLengthForPreviewKeepsFpcalcsMeasuredDuration(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 130), "real-key").WithEndpoint(srv.URL)

	if _, err := id.Identify(context.Background(), "/tmp/preview.mp3", 0); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if got := gotForm.Get("duration"); got != "130" {
		t.Errorf("duration = %q, want 130 (fpcalc's own measured duration, since no hint was given)", got)
	}
}

func TestIdentify_KeepsResultAtExactlyTheScoreFloor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[
			{"id":"ac-floor","score":0.85,"recordings":[{"id":"mb-floor","title":"At Floor","duration":200}]}
		]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 200), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/floor.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if len(match.Results) != 1 || match.Results[0].ID != "ac-floor" {
		t.Fatalf("Results = %+v, want the result scored exactly at the floor kept", match.Results)
	}
}

func TestIdentify_TiedScoresKeepStableInputOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[
			{"id":"ac-first","score":0.9,"recordings":[{"id":"mb-first","title":"First","duration":200}]},
			{"id":"ac-second","score":0.9,"recordings":[{"id":"mb-second","title":"Second","duration":210}]}
		]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 200), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/tied.mp3", 0)
	if err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if len(match.Results) != 2 || match.Results[0].ID != "ac-first" || match.Results[1].ID != "ac-second" {
		t.Fatalf("Results = %+v, want tied scores to keep the committed input order [ac-first ac-second]", match.Results)
	}
}

const acoustIDBodyCap = 2 << 20

func lookupServerServing(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

func lookupBodyOfSize(t *testing.T, size int) string {
	t.Helper()
	valid := `{"status":"ok","results":[]}`
	if size < len(valid) {
		t.Fatalf("size %d too small for a valid body", size)
	}
	return valid + strings.Repeat(" ", size-len(valid))
}

func TestLookup_BodyJustUnderTheCapStillReturnsItsMatch(t *testing.T) {
	srv := lookupServerServing(lookupBodyOfSize(t, acoustIDBodyCap-1))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 100), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err != nil {
		t.Fatalf("a body under the cap must parse, got %v", err)
	}
	if match.AcoustID != "" {
		t.Errorf("AcoustID = %q, want empty for a results-less body", match.AcoustID)
	}
}

func TestLookup_OversizedBodyWhoseCappedPrefixIsValidJSONIsRejected(t *testing.T) {
	srv := lookupServerServing(lookupBodyOfSize(t, acoustIDBodyCap+1024))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDir(t, 100), "real-key").WithEndpoint(srv.URL)
	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err == nil {
		t.Fatalf("oversized body must error, not parse the truncated prefix; got match %+v", match)
	}
}

func TestLookup_BodyExactlyAtTheCapIsRejected(t *testing.T) {
	srv := lookupServerServing(lookupBodyOfSize(t, acoustIDBodyCap))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDir(t, 100), "real-key").WithEndpoint(srv.URL)
	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err == nil {
		t.Fatalf("a body at the cap must error; got match %+v", match)
	}
}

func matchingLookupBodyPaddedTo(t *testing.T, size int) string {
	t.Helper()
	valid := `{"status":"ok","results":[{"id":"ac-top","score":0.98,"recordings":[{"id":"mb-top","title":"Song One","duration":236}]}]}`
	if size < len(valid) {
		t.Fatalf("size %d too small for a valid body", size)
	}
	return valid + strings.Repeat(" ", size-len(valid))
}

func TestIdentify_OversizedBodyWhoseCappedPrefixHoldsAMatchIsRejected(t *testing.T) {
	srv := lookupServerServing(matchingLookupBodyPaddedTo(t, acoustIDBodyCap+1024))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDir(t, 236), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)

	if err == nil {
		t.Fatalf("an oversized body must error, not yield the prefix's match; got %+v", match)
	}
	if match.AcoustID != "" || slices.Contains(match.MBIDs, "mb-top") {
		t.Errorf("match = %+v, want the zero match on error", match)
	}
}

func TestIdentify_BodyExactlyAtTheCapHoldingAMatchIsRejected(t *testing.T) {
	srv := lookupServerServing(matchingLookupBodyPaddedTo(t, acoustIDBodyCap))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDir(t, 236), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)

	if err == nil {
		t.Fatalf("a body at the cap must error; got %+v", match)
	}
}

func TestIdentify_BodyJustUnderTheCapStillReturnsItsMatch(t *testing.T) {
	srv := lookupServerServing(matchingLookupBodyPaddedTo(t, acoustIDBodyCap-1))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDir(t, 236), "real-key").WithEndpoint(srv.URL)

	match, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)
	if err != nil {
		t.Fatalf("a body under the cap must parse, got %v", err)
	}
	if match.AcoustID != "ac-top" || !slices.Contains(match.MBIDs, "mb-top") {
		t.Errorf("match = %+v, want ac-top linked to mb-top", match)
	}
}

func TestLookup_OversizedBodyErrorNamesTheLookupResponse(t *testing.T) {
	srv := lookupServerServing(lookupBodyOfSize(t, acoustIDBodyCap+1024))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDir(t, 100), "real-key").WithEndpoint(srv.URL)

	_, err := id.Identify(context.Background(), "/tmp/full.mp3", 0)

	if err == nil {
		t.Fatal("oversized body must error, got nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "read acoustid response: ") {
		t.Errorf("err = %q, want it to name the read of the acoustid lookup response", msg)
	}
	if strings.Contains(msg, "cluster") {
		t.Errorf("err = %q, names the cluster response; want the lookup response", msg)
	}
}

func TestLookup_EndlessStreamingBodyReturnsAnErrorPromptly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
		chunk := []byte(strings.Repeat(" ", 64<<10))
		for {
			if r.Context().Err() != nil {
				return
			}
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 100), "real-key").WithEndpoint(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := id.Identify(ctx, "/tmp/full.mp3", 0)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("an endless body must error, got nil")
		}
		if ctx.Err() != nil {
			t.Fatalf("returned only because the context expired (%v); the cap must stop the read", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("lookup kept reading an endless body")
	}
}

func throttlingServer(t *testing.T, throttled int, okBody string) (*httptest.Server, *int) {
	t.Helper()
	hits := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*hits++
		if *hits <= throttled {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(okBody))
	}))
	t.Cleanup(srv.Close)
	return srv, hits
}

const throttleMatchBody = `{"status":"ok","results":[{"id":"ac-1","score":0.97,"recordings":[{"id":"mb-1"}]}]}`

func TestIdentify_AThrottledAnswerSurfacesErrIdentifyThrottledWithoutAnAdapterRetry(t *testing.T) {
	srv, hits := throttlingServer(t, 2, throttleMatchBody)
	id := newIdentifier(t, fakeFpcalcDir(t, 130), "real-key").WithEndpoint(srv.URL)

	_, err := id.Identify(context.Background(), "/tmp/a.mp3", 0)

	if !errors.Is(err, download.ErrIdentifyThrottled) {
		t.Fatalf("err = %v, want ErrIdentifyThrottled", err)
	}
	if *hits != 1 {
		t.Errorf("hits = %d, want 1: retrying a throttled call belongs to the transport", *hits)
	}
}

func TestIdentify_NoMatchIsZeroMatchAndServerErrorIsNotThrottled(t *testing.T) {
	srv, _ := throttlingServer(t, 0, `{"status":"ok","results":[]}`)
	id := newIdentifier(t, fakeFpcalcDir(t, 130), "real-key").WithEndpoint(srv.URL)
	match, err := id.Identify(context.Background(), "/tmp/a.mp3", 0)
	if err != nil || match.Known() {
		t.Fatalf("no-match = %+v, %v; want zero match and nil error", match, err)
	}

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer failing.Close()
	id = newIdentifier(t, fakeFpcalcDir(t, 130), "real-key").WithEndpoint(failing.URL)
	_, err = id.Identify(context.Background(), "/tmp/a.mp3", 0)
	if err == nil || errors.Is(err, download.ErrIdentifyThrottled) {
		t.Errorf("500 err = %v, want a non-throttled error", err)
	}
}

func TestLookup_SendsAPIKeyInPostFormNotURL(t *testing.T) {
	const apiKey = "secret-api-key"

	var gotURL, gotMethod string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotMethod = r.Method
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[]}`))
	}))
	defer srv.Close()

	id := newIdentifier(t, fakeFpcalcDir(t, 100), apiKey).WithEndpoint(srv.URL)
	if _, err := id.Identify(context.Background(), "/tmp/full.mp3", 0); err != nil {
		t.Fatalf("Identify: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if strings.Contains(gotURL, apiKey) {
		t.Errorf("API key leaked into request URL %q", gotURL)
	}
	if got := gotForm.Get("client"); got != apiKey {
		t.Errorf("form client = %q, want %q", got, apiKey)
	}
}

func TestIdentify_EmptyFingerprintIsAnError(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits++ }))
	defer srv.Close()
	id := newIdentifier(t, fakeFpcalcDirs[emptyFingerprint], "real-key").WithEndpoint(srv.URL)

	_, err := id.Identify(context.Background(), "/tmp/broken.mp3", 0)

	if !errors.Is(err, domain.ErrEmptyFingerprint) {
		t.Fatalf("err = %v, want ErrEmptyFingerprint", err)
	}
	if hits != 0 {
		t.Errorf("lookup hits = %d, want 0 when there is no fingerprint", hits)
	}
}
