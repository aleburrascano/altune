// Package providermetrics counts go-api's outbound provider HTTP calls at the
// single shared transport seam. A CountingTransport wraps the provider
// http.RoundTripper, classifying each round trip by provider (mapped from the
// request host) and by outcome (ok / quota / error), then delegating
// transparently. The counters are process-global expvar integers over a fixed
// (provider, outcome) key set, exposed operator-only through
// GET /admin/metrics/live. Only host-derived provider names and outcome labels
// are ever recorded: never a URL, query, or any request/response body — so the
// counts carry no PII.
package providermetrics

import (
	"expvar"
	"net/http"
	"strings"
)

// Provider labels. A fixed set plus providerOther keeps the counter key space
// bounded: an unrecognized host can never grow new keys.
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

// Outcome labels.
const (
	outcomeOK    = "ok"
	outcomeQuota = "quota"
	outcomeError = "error"
)

// providerNames is the fixed, ordered provider key set (including other). It
// bounds the counter map and drives the snapshot.
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

// hostSuffixes maps a request host to a provider by domain suffix, so every
// subdomain and CDN of a provider (api./www./cdn./image hosts) folds to one
// key. Only a domain the provider itself owns may appear: a neutral CDN's
// domain (fastly.net, cloudfront.net) would bill every tenant's traffic to one
// provider, so a provider's host on such a CDN is listed in full instead.
//
// The first match wins, so a suffix nested inside another is listed before it:
// itunes.apple.com is the iTunes Search API, not Apple Music.
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

// counters holds one process-global expvar.Int per (provider, outcome). It is
// built once at package init; expvar.NewInt panics on a duplicate name, so the
// fixed key set is registered exactly here.
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

// CountingTransport wraps a base http.RoundTripper, counting each round trip by
// provider and outcome before returning the base's response and error
// unchanged. It is a transparent delegate: it never mutates the request,
// response, or error.
type CountingTransport struct {
	base http.RoundTripper
}

var _ http.RoundTripper = (*CountingTransport)(nil)

// NewCountingTransport returns a CountingTransport delegating to base.
func NewCountingTransport(base http.RoundTripper) *CountingTransport {
	return &CountingTransport{base: base}
}

// RoundTrip delegates to the base transport, then records one (provider,
// outcome) count. The base's response and error are returned verbatim.
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

// hostOf extracts the request host (no port), the only request-derived value
// this package reads. It never touches the path, query, headers, or body.
func hostOf(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return req.URL.Hostname()
}

// providerForHost folds a host to its provider label, or providerOther when no
// known suffix matches.
func providerForHost(host string) string {
	host = strings.ToLower(host)
	for _, e := range hostSuffixes {
		if host == e.suffix || strings.HasSuffix(host, "."+e.suffix) {
			return e.provider
		}
	}
	return providerOther
}

// outcomeFor classifies a round trip: 429 is the quota refusal, since that is
// the one status a provider uses to say the plan or the rate is spent;
// transport failures and every other 4xx/5xx are errors, everything else is ok.
// Counting a 404 or a 401 as quota would read as an exhausted provider on
// /admin/metrics/live when it is a wrong id or a bad key.
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

// Outcomes is the per-provider outcome breakdown, shaped for JSON exposure.
type Outcomes struct {
	OK    int64 `json:"ok"`
	Quota int64 `json:"quota"`
	Error int64 `json:"error"`
}

// Snapshot is a point-in-time read of the per-provider call counts, keyed by
// provider label. It carries only provider names and integer counts — no host,
// URL, or query text.
type Snapshot map[string]Outcomes

// ReadSnapshot returns the current per-provider, per-outcome counts. It reads
// only the package-scope expvar counters, so callers expose these specific
// counters without reaching the raw expvar registry (which also publishes
// process globals like cmdline and memstats).
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
