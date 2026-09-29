package providermetrics

import (
	"expvar"
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
}

var counters = newCounters()

func newCounters() map[string]map[string]*expvar.Int {
	m := make(map[string]map[string]*expvar.Int, len(providerNames))
	for _, p := range providerNames {
		om := make(map[string]*expvar.Int, len(outcomeNames))
		for _, o := range outcomeNames {
			om[o] = expvar.NewInt(varName(p, o))
		}
		m[p] = om
	}
	return m
}

func varName(provider, outcome string) string {
	return "discovery_provider_" + provider + "_" + outcome + "_total"
}

type CountingTransport struct {
	base http.RoundTripper
}

var _ http.RoundTripper = (*CountingTransport)(nil)

func NewCountingTransport(base http.RoundTripper) *CountingTransport {
	return &CountingTransport{base: base}
}

func (t *CountingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	record(providerForHost(hostOf(req)), outcomeFor(resp, err))
	return resp, err
}

func record(provider, outcome string) {
	counters[provider][outcome].Add(1)
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
	if resp.StatusCode >= 400 {
		return outcomeError
	}
	return outcomeOK
}

type Outcomes struct {
	OK    int64 `json:"ok"`
	Quota int64 `json:"quota"`
	Error int64 `json:"error"`
}

type Snapshot map[string]Outcomes

func ReadSnapshot() Snapshot {
	s := make(Snapshot, len(providerNames))
	for _, p := range providerNames {
		s[p] = Outcomes{
			OK:    counters[p][outcomeOK].Value(),
			Quota: counters[p][outcomeQuota].Value(),
			Error: counters[p][outcomeError].Value(),
		}
	}
	return s
}
