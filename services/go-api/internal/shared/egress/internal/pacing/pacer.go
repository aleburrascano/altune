package pacing

import (
	"net/http"
	"sync"

	"golang.org/x/time/rate"
)

var providerRateLimits = map[string]rate.Limit{
	"musicbrainz.org":            1,
	"itunes.apple.com":           0.5,
	"ws.audioscrobbler.com":      5,
	"music.youtube.com":          2,
	"api.discogs.com":            1,
	"api.deezer.com":             5,
	"api-v2.soundcloud.com":      3,
	"na.web.skill.music.a2z.com": 2,
	"api-partner.spotify.com":    3,
	"api.genius.com":             2,
	"sheets.googleapis.com":      2,
	"webservice.fanart.tv":       2,
	"coverartarchive.org":        2,
	"api.music.apple.com":        3,
	"open.spotify.com":           3,
	"auth.deezer.com":            1,
	"pipe.deezer.com":            3,
	"theaudiodb.com":             0.5,
	"music.amazon.com":           2,
	"music.apple.com":            2,
	"clienttoken.spotify.com":    2,
	"soundcloud.com":             2,
	"imgur.gg":                   2,
	"krakenfiles.com":            2,
}

const defaultProviderBurst = 4

var providerBursts = map[string]int{
	"musicbrainz.org": 1,
}

type Pacer struct {
	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

func New() *Pacer {
	return &Pacer{limiters: map[string]*rate.Limiter{}}
}

func (p *Pacer) Wait(req *http.Request) error {
	l := p.limiter(req.URL.Host)
	if l == nil {
		return nil
	}
	return l.Wait(req.Context())
}

func (p *Pacer) limiter(host string) *rate.Limiter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if l, ok := p.limiters[host]; ok {
		return l
	}
	var l *rate.Limiter
	if lim, ok := providerRateLimits[host]; ok {
		l = rate.NewLimiter(lim, providerBurst(host))
	}
	p.limiters[host] = l
	return l
}

func providerBurst(host string) int {
	if b, ok := providerBursts[host]; ok {
		return b
	}
	return defaultProviderBurst
}
