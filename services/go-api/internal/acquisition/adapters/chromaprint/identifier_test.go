package chromaprint

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

func TestBestMatch_PicksHighestScoreAboveThreshold(t *testing.T) {
	var parsed lookupResponse
	parsed.Status = "ok"
	parsed.Results = []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	}{
		{ID: "weak", Score: 0.9, Recordings: []struct {
			ID string `json:"id"`
		}{{ID: "mbid-a"}}},
		{ID: "strong", Score: 0.98, Recordings: []struct {
			ID string `json:"id"`
		}{{ID: "mbid-b"}}},
	}

	got := bestMatch(parsed)

	if got.AcoustID != "strong" {
		t.Errorf("AcoustID = %q, want strong", got.AcoustID)
	}
	if !got.Matches("mbid-b") {
		t.Errorf("MBIDs = %v, want mbid-b", got.MBIDs)
	}
}

func TestBestMatch_IgnoresLowScores(t *testing.T) {
	var parsed lookupResponse
	parsed.Status = "ok"
	parsed.Results = []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	}{
		{ID: "weak", Score: 0.3, Recordings: []struct {
			ID string `json:"id"`
		}{{ID: "mbid-a"}}},
	}

	if got := bestMatch(parsed); got.Known() {
		t.Errorf("a below-threshold result must not count as known, got %+v", got)
	}
}

func TestBestMatch_KeepsResultsWithOnlyAnAcoustID(t *testing.T) {
	var parsed lookupResponse
	parsed.Status = "ok"
	parsed.Results = []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	}{
		{ID: "acoustid-only", Score: 0.99},
	}

	got := bestMatch(parsed)
	if !got.Known() {
		t.Fatalf("an AcoustID with no MusicBrainz links still identifies the audio for cluster comparison, got %+v", got)
	}
	if !got.InCluster([]string{"other", "acoustid-only"}) {
		t.Errorf("InCluster must match on the AcoustID, got %+v", got)
	}
	if got.InCluster([]string{"other"}) {
		t.Error("an AcoustID outside the expected cluster is a different recording")
	}
}

func TestRecordingMatch_EmptyAcoustIDNeverMatchesACluster(t *testing.T) {
	if (ports.RecordingMatch{MBIDs: []string{"mb"}}).InCluster([]string{"", "a"}) {
		t.Error("a match with no AcoustID must never be read as being in the cluster")
	}
}

func TestAcoustIDsFor_SendsAPIKeyInPostFormNotURL(t *testing.T) {
	const apiKey = "secret-api-key"
	const mbid = "mbid-123"

	var gotURL string
	var gotMethod string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		gotMethod = r.Method
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tracks":[{"id":"track-1"}]}`))
	}))
	defer srv.Close()

	id := NewIdentifier("", apiKey).WithClusterEndpoint(srv.URL)

	ids, err := id.AcoustIDsFor(context.Background(), mbid)
	if err != nil {
		t.Fatalf("AcoustIDsFor: %v", err)
	}
	if len(ids) != 1 || ids[0] != "track-1" {
		t.Errorf("ids = %v, want [track-1]", ids)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if strings.Contains(gotURL, apiKey) {
		t.Errorf("API key leaked into request URL %q", gotURL)
	}
	if strings.Contains(gotURL, mbid) {
		t.Errorf("mbid leaked into request URL %q", gotURL)
	}
	if got := gotForm.Get("client"); got != apiKey {
		t.Errorf("form client = %q, want %q", got, apiKey)
	}
	if got := gotForm.Get("mbid"); got != mbid {
		t.Errorf("form mbid = %q, want %q", got, mbid)
	}
}

func TestNewIdentifier_DefaultsToAcoustIDEndpoint(t *testing.T) {
	id := NewIdentifier("", "key")
	if id.endpoint != defaultEndpoint {
		t.Errorf("endpoint = %q, want %q", id.endpoint, defaultEndpoint)
	}
	if id.client == nil {
		t.Error("expected an http client")
	}
}

// warnCapture records slog records at Warn or above so a test can assert that a
// misconfiguration announced itself at startup rather than staying silent.
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

// A misconfigured (empty) key must fail loudly at startup — the constructor
// logs a warning — instead of degrading silently until the first lookup.
func TestNewIdentifier_EmptyKeyWarnsAtStartup(t *testing.T) {
	logs := captureWarnings(t)

	NewIdentifier("", "")

	if !logs.warned {
		t.Fatal("an empty AcoustID API key must warn at startup, got no warning")
	}
}

// A whitespace-only key is just as misconfigured as an empty one and must not
// masquerade as a usable secret.
func TestNewIdentifier_WhitespaceOnlyKeyWarnsAndDisables(t *testing.T) {
	logs := captureWarnings(t)

	id := NewIdentifier("", "   \t\n ")

	if !logs.warned {
		t.Fatal("a whitespace-only AcoustID API key must warn at startup, got no warning")
	}
	if id.Available() {
		t.Error("a whitespace-only key must not report the identifier as available")
	}
}

// A genuine configuration stays quiet: a real key produces no startup warning.
func TestNewIdentifier_ValidKeyDoesNotWarn(t *testing.T) {
	logs := captureWarnings(t)

	NewIdentifier("", "real-key")

	if logs.warned {
		t.Errorf("a valid API key must not warn, got %v", logs.msgs)
	}
}

// At first use a missing key must return ErrMissingAPIKey, not a silent empty
// match, so a misconfig stays distinguishable from a genuine no-match.
func TestLookup_EmptyKeyReturnsError(t *testing.T) {
	id := NewIdentifier("", "")

	_, err := id.lookup(context.Background(), fingerprint{Duration: 100, Fingerprint: "abc"})

	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("lookup with empty key err = %v, want ErrMissingAPIKey", err)
	}
}

func TestAcoustIDsFor_EmptyKeyReturnsError(t *testing.T) {
	id := NewIdentifier("", "")

	_, err := id.AcoustIDsFor(context.Background(), "mbid-1")

	if !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("AcoustIDsFor with empty key err = %v, want ErrMissingAPIKey", err)
	}
}

// The regression's flip side: a valid key with no results is a genuine no-match
// — an empty slice and a nil error — and must stay distinct from a misconfig.
func TestAcoustIDsFor_ValidKeyNoResultsIsNilError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","tracks":[]}`))
	}))
	defer srv.Close()

	id := NewIdentifier("", "real-key").WithClusterEndpoint(srv.URL)

	ids, err := id.AcoustIDsFor(context.Background(), "mbid-1")
	if err != nil {
		t.Fatalf("a genuine no-match must not error, got %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("ids = %v, want empty", ids)
	}
}
