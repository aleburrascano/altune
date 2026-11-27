package downloadtest

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
)

type CandidateOption func(*ports.AudioCandidate)

func Candidate(url string, opts ...CandidateOption) ports.AudioCandidate {
	c := ports.AudioCandidate{URL: url}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

func CandidatePtr(url string, opts ...CandidateOption) *ports.AudioCandidate {
	c := Candidate(url, opts...)
	return &c
}

func ResolvedCandidate(url string, opts ...CandidateOption) ports.AudioCandidate {
	return Candidate(url, append([]CandidateOption{WithKind(ports.KindArtTrack)}, opts...)...)
}

func WithTitle(title string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Title = title }
}

func WithDuration(seconds float64) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Duration = seconds }
}

func WithChannel(channel string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Channel = channel }
}

func WithCategories(categories []string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Categories = categories }
}

func WithViewCount(views int64) CandidateOption {
	return func(c *ports.AudioCandidate) { c.ViewCount = views }
}

func WithSource(source acqdomain.SourceName) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Source = source }
}

func WithExplicit(explicit bool) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Explicit = explicit }
}

func WithObservedOMV(observed bool) CandidateOption {
	return func(c *ports.AudioCandidate) { c.ObservedOMV = observed }
}

func WithUnplayable(reason string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Unplayable = reason }
}

func WithArtists(artists []string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Artists = artists }
}

func WithAlbum(album string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Album = album }
}

func WithKind(kind ports.CandidateKind) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Kind = kind }
}
