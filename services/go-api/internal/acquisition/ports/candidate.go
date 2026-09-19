package ports

import (
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

// DedupeCandidatesBySourceKey keeps one candidate per recording, so the same
// video offered under two URL spellings cannot spend two of the caller's
// download attempts. The first spelling holds its position; a resolved
// duplicate takes that position from an unresolved one, because resolution is
// provenance the ranking downstream cannot recover.
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

// SourceKey canonicalizes a candidate URL so one recording under two spellings
// — music.youtube.com and www.youtube.com for a single video — is one key. The
// key is persisted as a track's exclude key, so its shape cannot change for a
// URL it already keys.
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

// EnoughCandidates mirrors the acquisition service's download-attempt cap: a
// search that has merged this many has nothing left to win, because the ranking
// downstream never reaches past that many candidates.
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

// CollectCandidatesUntilEnough stops running sources once enough candidates have
// merged. Sources are ordered best-first, so the runs it skips would each have
// paid their own process spawn and timeout to grow a tail the caller never
// reads.
func CollectCandidatesUntilEnough(
	n, enough int,
	run func(i int) ([]AudioCandidate, error),
	onSuccess func(i int, candidates []AudioCandidate),
	onFailure func(i int, err error),
	allFailed func(firstErr error) error,
) ([]AudioCandidate, error) {
	positionByKey := make(map[string]int)
	var merged []AudioCandidate
	var firstErr error
	failures := 0

	for i := 0; i < n && len(merged) < enough; i++ {
		candidates, err := run(i)
		if err != nil {
			failures++
			if firstErr == nil {
				firstErr = err
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
	return merged, nil
}
