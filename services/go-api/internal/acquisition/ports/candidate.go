package ports

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
)

type AudioCandidate struct {
	Title      string
	Duration   float64
	URL        string
	Channel    string
	Categories []string
	ViewCount  int64
	Source     string
	Resolved   bool
}

func DedupeCandidatesBySourceKey(merged, results []AudioCandidate, positionByKey map[string]int) []AudioCandidate {
	for _, c := range results {
		if c.URL == "" {
			continue
		}
		merged = mergeCandidate(merged, c, positionByKey)
	}
	return merged
}

func mergeCandidate(merged []AudioCandidate, c AudioCandidate, positionByKey map[string]int) []AudioCandidate {
	key := SourceKey(c.URL)
	position, isDuplicate := positionByKey[key]
	if !isDuplicate {
		positionByKey[key] = len(merged)
		return append(merged, c)
	}
	if c.Resolved && !merged[position].Resolved {
		merged[position] = c
	}
	return merged
}

var youtubeHosts = map[string]bool{
	"youtube.com":       true,
	"www.youtube.com":   true,
	"m.youtube.com":     true,
	"music.youtube.com": true,
	shortYouTubeHost:    true,
}

const shortYouTubeHost = "youtu.be"

var videoIDPathPrefixes = map[string]bool{"embed": true, "v": true}

var videoIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func SourceKey(rawURL string) string {
	if id := youtubeVideoID(rawURL); id != "" {
		return "youtube:" + id
	}
	return normalizedURLKey(rawURL)
}

func youtubeVideoID(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if !youtubeHosts[host] {
		return ""
	}
	return videoIDOnYouTubeHost(host, u)
}

func videoIDOnYouTubeHost(host string, u *url.URL) string {
	segments := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case host == shortYouTubeHost:
		return videoID(segments[0])
	case len(segments) == 1 && segments[0] == "watch":
		return videoID(u.Query().Get("v"))
	case len(segments) == 2 && videoIDPathPrefixes[segments[0]]:
		return videoID(segments[1])
	}
	return ""
}

func videoID(candidateID string) string {
	if !videoIDPattern.MatchString(candidateID) {
		return ""
	}
	return candidateID
}

func normalizedURLKey(rawURL string) string {
	key := strings.TrimSpace(rawURL)
	if i := strings.IndexByte(key, '?'); i >= 0 {
		key = key[:i]
	}
	key = strings.TrimPrefix(key, "https://")
	key = strings.TrimPrefix(key, "http://")
	key = strings.TrimPrefix(key, "www.")
	key = strings.TrimSuffix(key, "/")
	return strings.ToLower(key)
}

const EnoughCandidates = 8

const unlimitedCandidates = math.MaxInt

func CollectCandidates(
	n int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	return CollectCandidatesUntilEnough(n, unlimitedCandidates, run, onSuccess, onFailure, allFailed)
}

func CollectCandidatesUntilEnough(
	n, enough int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	positionByKey := make(map[string]int)
	var merged []AudioCandidate
	var firstErr, firstUnavailable error
	failures := 0

	for i := 0; i < n && len(merged) < enough; i++ {
		candidates, err := run(i)
		if err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
			}
			if firstUnavailable == nil && IsSourceUnavailable(err) {
				firstUnavailable = err
			}
			onFailure(i, err)
			continue
		}
		onSuccess(i, candidates)
		merged = DedupeCandidatesBySourceKey(merged, candidates, positionByKey)
	}

	if failures == n && firstErr != nil {
		return nil, allFailed(firstErr)
	}
	if len(merged) == 0 && firstUnavailable != nil {
		return nil, fmt.Errorf("no candidates and a source was unavailable: %w", firstUnavailable)
	}
	return merged, nil
}
