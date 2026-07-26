package chromaprint

import (
	"testing"
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

func TestBestMatch_IgnoresResultsWithNoRecordings(t *testing.T) {
	var parsed lookupResponse
	parsed.Status = "ok"
	parsed.Results = []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	}{
		{ID: "no-recordings", Score: 0.99},
	}

	if got := bestMatch(parsed); got.Known() {
		t.Errorf("a result carrying no recording ids is unknown, got %+v", got)
	}
}

func TestResolveBinary_FallsBackToPath(t *testing.T) {
	if got := resolveBinary("fpcalc", ""); got != "fpcalc" {
		t.Errorf("resolveBinary = %q, want the bare name for PATH lookup", got)
	}
	if got := resolveBinary("fpcalc", t.TempDir()); got != "fpcalc" {
		t.Errorf("resolveBinary = %q, want the bare name when the dir has no binary", got)
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
