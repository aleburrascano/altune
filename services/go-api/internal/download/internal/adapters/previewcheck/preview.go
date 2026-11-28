package previewcheck

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/childprocess"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/music"
	"altune/go-api/internal/shared/resilience"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const (
	BaseURL           = "https://api.deezer.com"
	deezerUserAgent   = "altune-deezerpreview/1.0"
	deezerQuotaCode   = 4
	deezerNoDataCode  = 800
	deezerAttempts    = 5
	deezerBackoff     = 10 * time.Second
	deezerTrackCap    = 1 << 20
	previewAudioCap   = 4 << 20
	rawPrintTimeout   = 90 * time.Second
	rawPrintSeconds   = 1200
	previewFilePrefix = "deezerpreview-"
)

var deezerPause = resilience.MustCappedBackoff(deezerBackoff, deezerBackoff<<(deezerAttempts-1))

var ErrDeezerThrottled = errors.New("deezer is still throttling after retries")

const (
	noLookupKey       = "no ISRC or Deezer track id to look a preview up by"
	unknownToDeezer   = "deezer does not know this track"
	noPreviewAtDeezer = "deezer has no preview for this track"
)

const FingerprintedItemCap = 9500

type Clip struct {
	DeezerTrackID string
	DeezerTitle   string
	Print         []uint32
	Unavailable   string
}

type Target struct {
	ISRC          string
	DeezerTrackID string
}

type RawFingerprint interface {
	Print(ctx context.Context, audioPath string) ([]uint32, error)
}

type FpcalcPrinter struct{ binary string }

func NewFpcalcPrinter(binary string) FpcalcPrinter { return FpcalcPrinter{binary: binary} }

type rawPrintBody struct {
	Fingerprint []uint32 `json:"fingerprint"`
}

func (f FpcalcPrinter) Print(ctx context.Context, audioPath string) ([]uint32, error) {
	stdout, stderr, err := childprocess.RunWithTimeout(ctx, rawPrintTimeout, f.binary, "-raw", "-json", "-length", strconv.Itoa(rawPrintSeconds), audioPath)
	if err != nil {
		return nil, fmt.Errorf("fpcalc -raw: %w (stderr: %s)", err, stderr)
	}
	var body rawPrintBody
	if err := json.Unmarshal([]byte(stdout), &body); err != nil {
		return nil, fmt.Errorf("parse fpcalc -raw output: %w", err)
	}
	if len(body.Fingerprint) == 0 {
		return nil, ErrEmptyPrint
	}
	return body.Fingerprint, nil
}

type cooldown struct {
	mu    sync.Mutex
	until time.Time
	now   clock.Func
}

func (c *cooldown) extendTo(deadline time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if deadline.After(c.until) {
		c.until = deadline
	}
}

func (c *cooldown) remaining() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.until.Sub(c.now())
}

type Previews struct {
	client  *http.Client
	baseURL string
	backoff resilience.BackoffPolicy
	pause   cooldown
	printer RawFingerprint
	sleep   clock.Sleeper
	now     clock.Func
}

var (
	ErrSleeperRequired = errors.New("previewcheck: the Deezer client needs a sleeper")
	ErrClockRequired   = errors.New("previewcheck: the Deezer client needs a clock")
)

func New(client *http.Client, baseURL string, printer RawFingerprint, sleep clock.Sleeper, now clock.Func) (*Previews, error) {
	if sleep == nil {
		return nil, fmt.Errorf("NewDeezerPreview: %w", ErrSleeperRequired)
	}
	if now == nil {
		return nil, fmt.Errorf("NewDeezerPreview: %w", ErrClockRequired)
	}
	return &Previews{client: client, baseURL: baseURL, backoff: deezerPause, printer: printer, sleep: sleep, now: now, pause: cooldown{now: now}}, nil
}

func MustNew(client *http.Client, baseURL string, printer RawFingerprint, sleep clock.Sleeper, now clock.Func) *Previews {
	previews, err := New(client, baseURL, printer, sleep, now)
	if err != nil {
		panic(err)
	}
	return previews
}

type deezerTrackBody struct {
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
	ID      int64  `json:"id"`
	Title   string `json:"title"`
	Preview string `json:"preview"`
}

func trackPath(identity Target) string {
	switch {
	case identity.ISRC != "":
		return "/track/isrc:" + url.PathEscape(music.CanonicalISRC(identity.ISRC))
	case identity.DeezerTrackID != "":
		return "/track/" + url.PathEscape(identity.DeezerTrackID)
	}
	return ""
}

func (d *Previews) Clip(ctx context.Context, identity Target) (Clip, error) {
	path := trackPath(identity)
	if path == "" {
		return Clip{Unavailable: noLookupKey}, nil
	}
	track, err := d.track(ctx, path)
	if err != nil {
		return Clip{}, err
	}
	if reason := track.unavailability(); reason != "" {
		return Clip{Unavailable: reason}, nil
	}
	prints, err := d.fingerprintPreview(ctx, track.Preview)
	if err != nil {
		return Clip{}, err
	}
	return Clip{DeezerTrackID: strconv.FormatInt(track.ID, 10), DeezerTitle: track.Title, Print: prints}, nil
}

func (b deezerTrackBody) unavailability() string {
	switch {
	case b.Error != nil:
		return unknownToDeezer
	case b.Preview == "":
		return noPreviewAtDeezer
	}
	return ""
}

func (d *Previews) track(ctx context.Context, path string) (deezerTrackBody, error) {
	raw, err := d.get(ctx, d.baseURL+path, deezerTrackCap, isDeezerQuota)
	if err != nil {
		return deezerTrackBody{}, err
	}
	var body deezerTrackBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return deezerTrackBody{}, fmt.Errorf("decode deezer %s: %w", path, err)
	}
	if body.Error != nil && body.Error.Code != deezerNoDataCode {
		return deezerTrackBody{}, fmt.Errorf("deezer %s: error code %d", path, body.Error.Code)
	}
	return body, nil
}

func (d *Previews) fingerprintPreview(ctx context.Context, previewURL string) ([]uint32, error) {
	audio, err := d.get(ctx, previewURL, previewAudioCap, noQuotaBody)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", previewFilePrefix)
	if err != nil {
		return nil, fmt.Errorf("create scratch dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	file := filepath.Join(dir, "preview.mp3")
	if err := os.WriteFile(file, audio, 0o600); err != nil {
		return nil, fmt.Errorf("write preview: %w", err)
	}
	r0, err := d.printer.Print(ctx, file)
	if err != nil {
		return r0, fmt.Errorf("fingerprint audio: %w", err)
	}
	return r0, nil
}

func noQuotaBody(int, []byte) bool { return false }

func isDeezerQuota(_ int, body []byte) bool {
	var parsed deezerTrackBody
	return json.Unmarshal(body, &parsed) == nil && parsed.Error != nil && parsed.Error.Code == deezerQuotaCode
}

func (d *Previews) get(ctx context.Context, target string, bodyCap int64, isThrottled func(int, []byte) bool) ([]byte, error) {
	for attempt := 1; attempt <= deezerAttempts; attempt++ {
		status, body, err := d.getOnce(ctx, target, bodyCap)
		switch {
		case err != nil:
			return nil, err
		case status == http.StatusTooManyRequests:
			d.pause.extendTo(d.now().Add(d.backoff.Ceiling(1)))
			return nil, ErrDeezerThrottled
		case isThrottled(status, body):
			d.pause.extendTo(d.now().Add(d.backoff.Ceiling(attempt)))
		case status != http.StatusOK:
			return nil, fmt.Errorf("deezer %s: status %d", target, status)
		default:
			return body, nil
		}
	}
	return nil, ErrDeezerThrottled
}

func (d *Previews) awaitTurn(ctx context.Context) error {
	if err := d.sleep(ctx, d.pause.remaining()); err != nil {
		return fmt.Errorf("wait out the deezer pause: %w", err)
	}
	return nil
}

func (d *Previews) getOnce(ctx context.Context, target string, bodyCap int64) (int, []byte, error) {
	if err := d.awaitTurn(ctx); err != nil {
		return 0, nil, err
	}
	status, body, err := outbound.GetBytesCapped(ctx, d.client, target, bodyCap, outbound.WithHeader("User-Agent", deezerUserAgent))
	var statusErr outbound.StatusError
	switch {
	case errors.As(err, &statusErr):
		return status, body, nil
	case err != nil:
		return 0, nil, fmt.Errorf("deezer %s: %w", target, err)
	}
	return status, body, nil
}
