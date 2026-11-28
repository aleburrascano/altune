package downloadtest

import (
	"altune/go-api/internal/download/internal/domain"
)

type CandidateOption func(*domain.AudioCandidate)

func Candidate(url string, opts ...CandidateOption) domain.AudioCandidate {
	c := domain.AudioCandidate{URL: url}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

func CandidatePtr(url string, opts ...CandidateOption) *domain.AudioCandidate {
	c := Candidate(url, opts...)
	return &c
}

func ResolvedCandidate(url string, opts ...CandidateOption) domain.AudioCandidate {
	return Candidate(url, append([]CandidateOption{WithKind(domain.KindArtTrack)}, opts...)...)
}

func WithTitle(title string) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Title = title }
}

func WithDuration(seconds float64) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Duration = seconds }
}

func WithChannel(channel string) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Channel = channel }
}

func WithCategories(categories []string) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Categories = categories }
}

func WithViewCount(views int64) CandidateOption {
	return func(c *domain.AudioCandidate) { c.ViewCount = views }
}

func WithSource(source domain.SourceName) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Source = source }
}

func WithExplicit(explicit bool) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Explicit = explicit }
}

func WithObservedOMV(observed bool) CandidateOption {
	return func(c *domain.AudioCandidate) { c.ObservedOMV = observed }
}

func WithUnplayable(reason string) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Unplayable = reason }
}

func WithArtists(artists []string) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Artists = artists }
}

func WithAlbum(album string) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Album = album }
}

func WithKind(kind domain.CandidateKind) CandidateOption {
	return func(c *domain.AudioCandidate) { c.Kind = kind }
}
