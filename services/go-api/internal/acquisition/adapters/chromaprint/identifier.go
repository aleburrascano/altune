package chromaprint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"altune/go-api/internal/acquisition/ports"
)

const (
	fingerprintTimeout = 60 * time.Second
	lookupTimeout      = 15 * time.Second
	minScore           = 0.85
	defaultEndpoint    = "https://api.acoustid.org/v2/lookup"
)

var _ ports.AudioIdentifier = (*Identifier)(nil)

type Identifier struct {
	fpcalc   string
	apiKey   string
	endpoint string
	client   *http.Client
}

func NewIdentifier(binDir, apiKey string) *Identifier {
	return &Identifier{
		fpcalc:   resolveBinary("fpcalc", binDir),
		apiKey:   apiKey,
		endpoint: defaultEndpoint,
		client:   &http.Client{Timeout: lookupTimeout},
	}
}

func (i *Identifier) WithEndpoint(endpoint string) *Identifier {
	i.endpoint = endpoint
	return i
}

func resolveBinary(name, binDir string) string {
	if binDir != "" {
		candidate := name
		if runtime.GOOS == "windows" {
			candidate = name + ".exe"
		}
		full := filepath.Join(binDir, candidate)
		if _, err := os.Stat(full); err == nil {
			return full
		}
	}
	return name
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
	fpCtx, cancel := context.WithTimeout(ctx, fingerprintTimeout)
	defer cancel()

	cmd := exec.CommandContext(fpCtx, i.fpcalc, "-json", filePath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fingerprint{}, fmt.Errorf("fpcalc: %w (stderr: %s)", err, stderr.String())
	}

	var fp fingerprint
	if err := json.Unmarshal(stdout.Bytes(), &fp); err != nil {
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
		if len(mbids) == 0 {
			continue
		}
		best = ports.RecordingMatch{AcoustID: result.ID, MBIDs: mbids, Score: result.Score}
	}
	return best
}
