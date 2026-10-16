package chromaprint

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/execcmd"
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

	"golang.org/x/time/rate"
)

var ErrMissingAPIKey = errors.New("chromaprint: AcoustID API key is empty")

const (
	fingerprintTimeout = 60 * time.Second
	lookupTimeout      = 15 * time.Second
	MinScore           = 0.85
	defaultEndpoint    = "https://api.acoustid.org/v2/lookup"
	requestsPerSecond  = 3
	lookupBodyCap      = 2 << 20
	defaultRetryAfter  = time.Second
	maxRetryAfter      = 5 * time.Second
)

var _ ports.AudioIdentifier = (*Identifier)(nil)

type Identifier struct {
	fpcalc   string
	apiKey   string
	endpoint string
	client   *http.Client
	limiter  *rate.Limiter
}

func NewIdentifier(binDir, apiKey string) *Identifier {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		slog.WarnContext(context.Background(), "chromaprint: AcoustID API key is empty or blank; "+
			"audio fingerprint identification is disabled and every lookup will fail")
	}
	return &Identifier{
		fpcalc:   binpath.Resolve("fpcalc", binDir),
		apiKey:   key,
		endpoint: defaultEndpoint,
		client:   &http.Client{Timeout: lookupTimeout},
		limiter:  rate.NewLimiter(rate.Limit(requestsPerSecond), requestsPerSecond),
	}
}

func (i *Identifier) WithRequestsPerSecond(perSecond float64) *Identifier {
	i.limiter = rate.NewLimiter(rate.Limit(perSecond), 1)
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

func (i *Identifier) Identify(ctx context.Context, filePath string, fullLengthForPreview float64) (ports.RecordingMatch, error) {
	fp, err := i.fingerprintFile(ctx, filePath)
	if err != nil {
		return ports.RecordingMatch{}, err
	}
	if fp.Fingerprint == "" || fp.Duration <= 0 {
		return ports.RecordingMatch{}, fmt.Errorf("%s: %w", filePath, acqdomain.ErrEmptyFingerprint)
	}
	if fullLengthForPreview > 0 {
		fp.Duration = math.Round(fullLengthForPreview)
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

type lookupRecording struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Duration float64 `json:"duration"`
	Artists  []struct {
		Name string `json:"name"`
	} `json:"artists"`
	ReleaseGroups []struct {
		ID             string   `json:"id"`
		Title          string   `json:"title"`
		Type           string   `json:"type"`
		SecondaryTypes []string `json:"secondarytypes"`
	} `json:"releasegroups"`
}

type lookupResponse struct {
	Status string `json:"status"`
	Error  struct {
		Message string `json:"message"`
	} `json:"error"`
	Results []struct {
		ID         string            `json:"id"`
		Score      float64           `json:"score"`
		Recordings []lookupRecording `json:"recordings"`
	} `json:"results"`
}

func (i *Identifier) lookup(ctx context.Context, fp fingerprint) (ports.RecordingMatch, error) {
	if i.apiKey == "" {
		return ports.RecordingMatch{}, ErrMissingAPIKey
	}

	form := url.Values{}
	form.Set("client", i.apiKey)
	form.Set("meta", "recordings releasegroups")
	form.Set("duration", strconv.Itoa(int(fp.Duration)))
	form.Set("fingerprint", fp.Fingerprint)

	resp, err := i.send(ctx, i.endpoint, form, "acoustid lookup")
	if err != nil {
		return ports.RecordingMatch{}, err
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

	return matchOf(acoustIDResults(parsed)), nil
}

func (i *Identifier) send(ctx context.Context, endpoint string, form url.Values, label string) (*http.Response, error) {
	resp, err := i.post(ctx, endpoint, form, label)
	if err != nil || resp.StatusCode != http.StatusTooManyRequests {
		return resp, err
	}
	wait := retryAfter(resp.Header.Get("Retry-After"))
	resp.Body.Close()

	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w", label, ctx.Err())
	case <-timer.C:
	}

	resp, err = i.post(ctx, endpoint, form, label)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: status %d: %w", label, resp.StatusCode, acqdomain.ErrIdentifyThrottled)
	}
	return resp, nil
}

func (i *Identifier) post(ctx context.Context, endpoint string, form url.Values, label string) (*http.Response, error) {
	if err := i.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", label, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return resp, nil
}

func retryAfter(header string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err != nil || seconds < 0 {
		return defaultRetryAfter
	}
	return time.Duration(min(seconds, int(maxRetryAfter/time.Second))) * time.Second
}

func matchOf(results []ports.AcoustIDResult) ports.RecordingMatch {
	for _, result := range results {
		mbids := make([]string, 0, len(result.Recordings))
		for _, rec := range result.Recordings {
			if rec.MBID != "" {
				mbids = append(mbids, rec.MBID)
			}
		}
		if result.ID == "" && len(mbids) == 0 {
			continue
		}
		return ports.RecordingMatch{AcoustID: result.ID, MBIDs: mbids, Score: result.Score, Results: results}
	}
	return ports.RecordingMatch{Results: results}
}

func acoustIDResults(full lookupResponse) []ports.AcoustIDResult {
	results := make([]ports.AcoustIDResult, 0, len(full.Results))
	for _, result := range full.Results {
		if result.Score < MinScore {
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

func linkedRecordings(recordings []lookupRecording) []ports.LinkedRecording {
	linked := make([]ports.LinkedRecording, 0, len(recordings))
	for _, recording := range recordings {
		linked = append(linked, ports.LinkedRecording{
			MBID:          recording.ID,
			Title:         recording.Title,
			Artists:       artistNames(recording.Artists),
			Duration:      recording.Duration,
			ReleaseGroups: releaseGroups(recording),
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

func releaseGroups(recording lookupRecording) []ports.ReleaseGroup {
	if len(recording.ReleaseGroups) == 0 {
		return nil
	}
	groups := make([]ports.ReleaseGroup, 0, len(recording.ReleaseGroups))
	for _, group := range recording.ReleaseGroups {
		groups = append(groups, ports.ReleaseGroup{
			MBID:           group.ID,
			Title:          group.Title,
			Type:           group.Type,
			SecondaryTypes: group.SecondaryTypes,
		})
	}
	return groups
}
