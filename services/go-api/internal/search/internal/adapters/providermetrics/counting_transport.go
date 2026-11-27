package providermetrics

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/shared/telemetry"
	"errors"
	"net/http"
	"strings"
)

const (
	providerDeezer      = "deezer"
	providerSpotify     = "spotify"
	providerSoundCloud  = "soundcloud"
	providerAppleMusic  = "applemusic"
	providerITunes      = "itunes"
	providerAmazonMusic = "amazonmusic"
	providerYouTube     = "youtube"
	providerMusicBrainz = "musicbrainz"
	providerLastFM      = "lastfm"
	providerDiscogs     = "discogs"
	providerGenius      = "genius"
	providerTheAudioDB  = "theaudiodb"
	providerFanart      = "fanart"
	providerSheets      = "sheets"
	providerOther       = "other"
)

const (
	outcomeOK    = "ok"
	outcomeQuota = "quota"
	outcomeError = "error"
)

var providerNames = []string{
	providerDeezer,
	providerSpotify,
	providerSoundCloud,
	providerAppleMusic,
	providerITunes,
	providerAmazonMusic,
	providerYouTube,
	providerMusicBrainz,
	providerLastFM,
	providerDiscogs,
	providerGenius,
	providerTheAudioDB,
	providerFanart,
	providerSheets,
	providerOther,
}

var outcomeNames = []string{outcomeOK, outcomeQuota, outcomeError}

var hostSuffixes = []struct {
	suffix   string
	provider string
}{
	{"deezer.com", providerDeezer},
	{"dzcdn.net", providerDeezer},
	{"spotify.com", providerSpotify},
	{"spotifycdn.com", providerSpotify},
	{"scdn.co", providerSpotify},
	{"soundcloud.com", providerSoundCloud},
	{"sndcdn.com", providerSoundCloud},
	{"itunes.apple.com", providerITunes},
	{"apple.com", providerAppleMusic},
	{"mzstatic.com", providerAppleMusic},
	{"amazon.com", providerAmazonMusic},
	{"a2z.com", providerAmazonMusic},
	{"youtube.com", providerYouTube},
	{"googleusercontent.com", providerYouTube},
	{"musicbrainz.org", providerMusicBrainz},
	{"coverartarchive.org", providerMusicBrainz},
	{"last.fm", providerLastFM},
	{"audioscrobbler.com", providerLastFM},
	{"lastfm.freetls.fastly.net", providerLastFM},
	{"discogs.com", providerDiscogs},
	{"genius.com", providerGenius},
	{"theaudiodb.com", providerTheAudioDB},
	{"fanart.tv", providerFanart},
	{"sheets.googleapis.com", providerSheets},
}

func retryVarName(provider string) string {
	return "discovery_provider_" + provider + "_retry_total"
}

func RetryRecorder(reg *telemetry.Registry) func(host string) {
	retries := make(map[string]*telemetry.Counter, len(providerNames))
	for _, p := range providerNames {
		retries[p] = reg.Counter(retryVarName(p))
	}
	return func(host string) {
		retries[providerForHost(host)].Add(1)
	}
}

func varName(provider, outcome string) string {
	return "discovery_provider_" + provider + "_" + outcome + "_total"
}

type CountingTransport struct {
	base     http.RoundTripper
	counters map[string]map[string]*telemetry.Counter
}

var _ http.RoundTripper = (*CountingTransport)(nil)

func NewCountingTransport(base http.RoundTripper, reg *telemetry.Registry) *CountingTransport {
	counters := make(map[string]map[string]*telemetry.Counter, len(providerNames))
	for _, p := range providerNames {
		byOutcome := make(map[string]*telemetry.Counter, len(outcomeNames))
		for _, o := range outcomeNames {
			byOutcome[o] = reg.Counter(varName(p, o))
		}
		counters[p] = byOutcome
	}
	return &CountingTransport{base: base, counters: counters}
}

func (t *CountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = outbound.DefaultTransport()
	}
	resp, err := base.RoundTrip(req)
	if errors.Is(err, ports.ErrProviderRateLimitQueueTimeout) {
		return resp, err
	}
	t.counters[providerForHost(hostOf(req))][outcomeFor(resp, err)].Add(1)
	return resp, err
}

func hostOf(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return req.URL.Hostname()
}

func providerForHost(host string) string {
	host = strings.ToLower(host)
	for _, e := range hostSuffixes {
		if host == e.suffix || strings.HasSuffix(host, "."+e.suffix) {
			return e.provider
		}
	}
	return providerOther
}

func outcomeFor(resp *http.Response, err error) string {
	if err != nil || resp == nil {
		return outcomeError
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return outcomeQuota
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return outcomeError
	}
	return outcomeOK
}

type Outcomes struct {
	OK      int64 `json:"ok"`
	Quota   int64 `json:"quota"`
	Error   int64 `json:"error"`
	Retries int64 `json:"retries"`
}

type Snapshot map[string]Outcomes

func ReadSnapshot(reg *telemetry.Registry) Snapshot {
	s := make(Snapshot, len(providerNames))
	for _, p := range providerNames {
		s[p] = Outcomes{
			OK:      reg.Counter(varName(p, outcomeOK)).Value(),
			Quota:   reg.Counter(varName(p, outcomeQuota)).Value(),
			Error:   reg.Counter(varName(p, outcomeError)).Value(),
			Retries: reg.Counter(retryVarName(p)).Value(),
		}
	}
	return s
}
