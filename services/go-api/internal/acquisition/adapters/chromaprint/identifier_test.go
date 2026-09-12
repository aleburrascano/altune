package chromaprint

import (
	"context"
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
