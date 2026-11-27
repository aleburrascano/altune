package downloadtest

import "altune/go-api/internal/download/internal/core/ports"

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

func OfKind(kind ports.CandidateKind) CandidateOption {
	return func(c *ports.AudioCandidate) { c.Kind = kind }
}
