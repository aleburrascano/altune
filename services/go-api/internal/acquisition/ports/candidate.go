package ports

import (
	"math"
	"net/url"
	"regexp"
	"strings"
)

type AudioCandidate struct {
	Title string
	// Duration is the length in seconds the source reported at search time, zero
	// when it reported none. Search metadata is advisory: it ranks candidates and
	// can rule one out before anything is downloaded, but the post-download probe
	// is the authoritative gate, and a zero simply leaves the candidate to it.
	Duration   float64
	URL        string
	Channel    string
	Categories []string
	ViewCount  int64
	Source     string
	// Resolved marks a candidate a catalog lookup produced from the track's own
	// recording identity, not from a text search. That provenance already
	// establishes the recording, so a resolved candidate skips the identity and
	// duration gates, ranks ahead of every searched candidate, and takes a
	// duplicate's position from an unresolved spelling of the same recording.
	Resolved bool
}

// DedupeCandidatesBySourceKey keeps one candidate per recording, so the same
// video offered under two URL spellings cannot spend two of the caller's
// download attempts. The first spelling holds its position; a resolved
// duplicate takes that position from an unresolved one, because resolution is
// provenance the ranking downstream cannot recover. A candidate with no URL is
// dropped, having no key and nothing fetchable. positionByKey indexes merged, so
// a caller folding several batches must pass the same map it built merged with;
// a fresh map would let a duplicate through.
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

// CollectCandidates folds n runs into one deduped list and treats partial
// failure as success: whatever arrived comes back with a nil error, because one
// dead source must not cost a track the others found. Only every run failing is
// an error, allFailed over the first failure seen, so a caller cannot read a nil
// error as "nothing failed" — onFailure, called inside the loop as each failure
// lands, is where a caller learns that. An n of zero yields no candidates and no
// error. No callback may be nil; each is called unguarded.
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
// reads. It carries CollectCandidates' failure contract: stopping early cannot
// itself produce an error, since a skipped run is not a failed one and only n
// failures out of n runs error at all.
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
