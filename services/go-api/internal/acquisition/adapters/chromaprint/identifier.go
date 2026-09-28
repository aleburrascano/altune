package chromaprint

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"

	"golang.org/x/time/rate"
)

var ErrMissingAPIKey = errors.New("chromaprint: AcoustID API key is empty")

const (
	fingerprintTimeout = 60 * time.Second
	lookupTimeout      = 15 * time.Second
	minScore           = 0.85
	defaultEndpoint    = "https://api.acoustid.org/v2/lookup"
	clusterEndpoint    = "https://api.acoustid.org/v2/track/list_by_mbid"
	requestsPerSecond  = 3
	lookupBodyCap      = 2 << 20
)

var _ ports.AudioIdentifier = (*Identifier)(nil)

type Identifier struct {
	fpcalc          string
	apiKey          string
	endpoint        string
	clusterEndpoint string
	client          *http.Client
	limiter         *rate.Limiter
}

func NewIdentifier(binDir, apiKey string) *Identifier {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		slog.Warn("chromaprint: AcoustID API key is empty or blank; " +
			"audio fingerprint identification is disabled and every lookup will fail")
	}
	return &Identifier{
		fpcalc:          binpath.Resolve("fpcalc", binDir),
		apiKey:          key,
		endpoint:        defaultEndpoint,
		clusterEndpoint: clusterEndpoint,
		client:          &http.Client{Timeout: lookupTimeout},
		limiter:         rate.NewLimiter(rate.Limit(requestsPerSecond), requestsPerSecond),
	}
}

func (i *Identifier) WithClusterEndpoint(endpoint string) *Identifier {
	i.clusterEndpoint = endpoint
	return i
}

func (i *Identifier) WithEndpoint(endpoint string) *Identifier {
	i.endpoint = endpoint
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

func (i *Identifier) Identify(ctx context.Context, filePath string, durationHint float64) (ports.RecordingMatch, error) {
	fp, err := i.fingerprintFile(ctx, filePath)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	if fp.Fingerprint == "" || fp.Duration <= 0 {
		return ports.RecordingMatch{}, nil
	}
	if durationHint > 0 {
		fp.Duration = math.Round(durationHint)
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
	Status string `json:"status"`
	Error  struct {
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
		return ports.RecordingMatch{}, ErrMissingAPIKey
	}
	if err := i.limiter.Wait(ctx); err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("acoustid lookup: %w", err)
	}

	form := url.Values{}
	form.Set("client", i.apiKey)
	form.Set("meta", "recordings releasegroups")
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, lookupBodyCap+1))
	if err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("read acoustid response: %w", err)
	}
	if len(body) >= lookupBodyCap {
		return ports.RecordingMatch{}, fmt.Errorf("read acoustid response: exceeds %d byte cap", lookupBodyCap)
	}

	var parsed lookupResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("parse acoustid response: %w", err)
	}
	if parsed.Status != "ok" {
		return ports.RecordingMatch{}, fmt.Errorf("acoustid lookup: %s", parsed.Error.Message)
	}

	var full fullLookupResponse
	if err := json.Unmarshal(body, &full); err != nil {
		return ports.RecordingMatch{}, fmt.Errorf("parse acoustid response: %w", err)
	}

	match := bestMatch(parsed)
	match.Results = acoustIDResults(full)
	return match, nil
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
	if i.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if mbid == "" {
		return nil, nil
	}
	if err := i.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("acoustid cluster lookup: %w", err)
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

	body, err := io.ReadAll(io.LimitReader(resp.Body, lookupBodyCap+1))
	if err != nil {
		return nil, fmt.Errorf("read acoustid cluster response: %w", err)
	}
	if len(body) >= lookupBodyCap {
		return nil, fmt.Errorf("read acoustid cluster response: exceeds %d byte cap", lookupBodyCap)
	}

	var parsed clusterResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
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

type fullLookupRecording struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Artists  []struct {
		Name string `json:"name"`
	} `json:"artists"`
}

type fullLookupResponse struct {
	Results []struct {
		ID         string                `json:"id"`
		Score      float64               `json:"score"`
		Recordings []fullLookupRecording `json:"recordings"`
	} `json:"results"`
}

func acoustIDResults(full fullLookupResponse) []ports.AcoustIDResult {
	results := make([]ports.AcoustIDResult, 0, len(full.Results))
	for _, result := range full.Results {
		if result.Score < minScore {
			continue
		}
		results = append(results, ports.AcoustIDResult{
			ID:         result.ID,
			Score:      result.Score,
			Recordings: linkedRecordings(result.Recordings),
		})
	}
	sort.SliceStable(results, func(a, b int) bool { return results[a].Score > results[b].Score })
	return results
}

func linkedRecordings(recordings []fullLookupRecording) []ports.LinkedRecording {
	linked := make([]ports.LinkedRecording, 0, len(recordings))
	for _, recording := range recordings {
		linked = append(linked, ports.LinkedRecording{
			MBID:     recording.ID,
			Title:    recording.Title,
			Artists:  artistNames(recording.Artists),
			Duration: recording.Duration,
		})
	}
	return linked
}

func artistNames(artists []struct {
	Name string `json:"name"`
},
) []string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if artist.Name != "" {
			names = append(names, artist.Name)
		}
	}
	return names
}
