package downloadtest

import (
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
)

type CandidateOption func(*ports.AudioCandidate)

func Candidate(url string, opts ...CandidateOption) ports.AudioCandidate {
	c := ports.AudioCandidate{Title: "Song", URL: url}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

func ResolvedCandidate(url string, opts ...CandidateOption) ports.AudioCandidate {
	return Candidate(url, append([]CandidateOption{OfKind(ports.KindArtTrack)}, opts...)...)
}

func Titled(title string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Title = title }
}

func Lasting(seconds float64) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Duration = seconds }
}

func OfKind(kind ports.CandidateKind) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Kind = kind }
}

func ByArtists(artists ...string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Artists = artists }
}

func OnAlbum(album string) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Album = album }
}

func Explicit() CandidateOption {
	return func(c *ports.AudioCandidate) { c.Explicit = true }
}

func FromSource(source acqdomain.SourceName) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Source = source }
}
