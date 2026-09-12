package chromaprint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"
)

const (
	fingerprintTimeout = 60 * time.Second
	lookupTimeout      = 15 * time.Second
	minScore           = 0.85
	defaultEndpoint    = "https://api.acoustid.org/v2/lookup"
	clusterEndpoint    = "https://api.acoustid.org/v2/track/list_by_mbid"
)

var _ ports.AudioIdentifier = (*Identifier)(nil)

type Identifier struct {
	fpcalc          string
	apiKey          string
	endpoint        string
	clusterEndpoint string
	client          *http.Client
}

func NewIdentifier(binDir, apiKey string) *Identifier {
	return &Identifier{
		fpcalc:          binpath.Resolve("fpcalc", binDir),
		apiKey:          apiKey,
		endpoint:        defaultEndpoint,
		clusterEndpoint: clusterEndpoint,
		client:          &http.Client{Timeout: lookupTimeout},
	}
}

func (i *Identifier) WithEndpoint(endpoint string) *Identifier {
	i.endpoint = endpoint
	return i
}

func (i *Identifier) WithClusterEndpoint(endpoint string) *Identifier {
	i.clusterEndpoint = endpoint
	return i
}

func (i *Identifier) Available() bool {
	if i.apiKey == "" {
		return false
	}
	return binpath.Runnable(i.fpcalc)
}

type fingerprint struct {
	Duration    float64 `json:"duration"`
	Fingerprint string  `json:"fingerprint"`
}

func (i *Identifier) Identify(ctx context.Context, filePath string) (ports.RecordingMatch, error) {
	fp, err := i.fingerprintFile(ctx, filePath)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	if fp.Fingerprint == "" || fp.Duration <= 0 {
		return ports.RecordingMatch{}, nil
	}
	return i.lookup(ctx, fp)
}

func (i *Identifier) fingerprintFile(ctx context.Context, filePath string) (fingerprint, error) {
	stdout, stderr, err := execcmd.RunWithTimeout(ctx, fingerprintTimeout, i.fpcalc, "-json", filePath)
	if err != nil {
		return fingerprint{}, fmt.Errorf("fpcalc: %w (stderr: %s)", err, stderr)
	}

	var fp fingerprint
	if err := json.Unmarshal([]byte(stdout), &fp); err != nil {
		return fingerprint{}, fmt.Errorf("parse fpcalc output: %w", err)
	}
	return fp, nil
}

type lookupResponse struct {
	Status  string `json:"status"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
	Results []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID string `json:"id"`
		} `json:"recordings"`
	} `json:"results"`
}

func (i *Identifier) lookup(ctx context.Context, fp fingerprint) (ports.RecordingMatch, error) {
	if i.apiKey == "" {
		return ports.RecordingMatch{}, nil
	}

	form := url.Values{}
	form.Set("client", i.apiKey)
	form.Set("meta", "recordingids")
	form.Set("duration", strconv.Itoa(int(fp.Duration)))
	form.Set("fingerprint", fp.Fingerprint)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("build acoustid request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := i.client.Do(req)
	if err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("acoustid lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ports.RecordingMatch{}, fmt.Errorf("acoustid lookup: status %d", resp.StatusCode)
	}

	var parsed lookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("parse acoustid response: %w", err)
	}
	if parsed.Status != "ok" {
		return ports.RecordingMatch{}, fmt.Errorf("acoustid lookup: %s", parsed.Error.Message)
	}

	return bestMatch(parsed), nil
}

type clusterResponse struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	Tracks []struct {
		ID string `json:"id"`
	} `json:"tracks"`
}

func (i *Identifier) AcoustIDsFor(ctx context.Context, mbid string) ([]string, error) {
	if i.apiKey == "" || mbid == "" {
		return nil, nil
	}

	form := url.Values{}
	form.Set("client", i.apiKey)
	form.Set("mbid", mbid)
	form.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, i.clusterEndpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build acoustid cluster request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("acoustid cluster lookup: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("acoustid cluster lookup: status %d", resp.StatusCode)
	}

	var parsed clusterResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse acoustid cluster response: %w", err)
	}
	if parsed.Status != "ok" {
		return nil, fmt.Errorf("acoustid cluster lookup: %s", parsed.Error.Message)
	}

	ids := make([]string, 0, len(parsed.Tracks))
	for _, track := range parsed.Tracks {
		if track.ID != "" {
			ids = append(ids, track.ID)
		}
	}
	return ids, nil
}

func bestMatch(parsed lookupResponse) ports.RecordingMatch {
	var best ports.RecordingMatch
	for _, result := range parsed.Results {
		if result.Score < minScore || result.Score <= best.Score {
			continue
		}
		mbids := make([]string, 0, len(result.Recordings))
		for _, rec := range result.Recordings {
			if rec.ID != "" {
				mbids = append(mbids, rec.ID)
			}
		}
		if result.ID == "" && len(mbids) == 0 {
			continue
		}
		best = ports.RecordingMatch{AcoustID: result.ID, MBIDs: mbids, Score: result.Score}
	}
	return best
}
