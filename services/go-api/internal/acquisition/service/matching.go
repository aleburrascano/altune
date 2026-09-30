package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
	"log/slog"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var featuredRe = regexp.MustCompile(`(?i)\b(?:featuring|feat|ft)\.?\s+([^()\[\]]+)`)

var featSepRe = regexp.MustCompile(`(?i)\s*(?:,|&|\band\b)\s*`)

func extractFeaturedArtists(title string) []string {
	m := featuredRe.FindStringSubmatch(title)
	if m == nil {
		return nil
	}
	parts := featSepRe.Split(strings.TrimSpace(m[1]), -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func featureMatch(trackTitle, candidateTitle string) bool {
	feats := extractFeaturedArtists(trackTitle)
	if len(feats) == 0 {
		return true
	}
	cand := strings.ToLower(candidateTitle)
	for _, f := range feats {
		if !strings.Contains(cand, strings.ToLower(f)) {
			return false
		}
	}
	return true
}

var qualifierRe = regexp.MustCompile(`[\(\[\{]([^\)\]\}]*)[\)\]\}]`)

func qualifierTokens(title string) map[string]bool {
	tokens := make(map[string]bool)
	for _, segment := range qualifierRe.FindAllStringSubmatch(norm.NFKC.String(title), -1) {
		if featuredRe.MatchString(segment[1]) {
			continue
		}
		for _, word := range strings.Fields(textnorm.NormalizeForMatch(segment[1])) {
			tokens[word] = true
		}
	}
	return tokens
}

func qualifierDistance(trackTitle, candidateTitle string) int {
	want := qualifierTokens(trackTitle)
	got := qualifierTokens(candidateTitle)

	distance := 0
	for token := range got {
		if !want[token] {
			distance += unrequestedQualifierCost
		}
	}
	for token := range want {
		if !got[token] {
			distance += unfulfilledQualifierCost
		}
	}
	return distance
}

type qualifierEntry struct {
	label  string
	veto   bool
	tokens []string
}

var qualifierLexicon = []qualifierEntry{
	{"instrumental", true, []string{"instrumental"}},
	{"karaoke", true, []string{"karaoke"}},
	{"a cappella", true, []string{"acapella"}},
	{"a cappella", true, []string{"a", "cappella"}},
	{"remix", true, []string{"remix"}},
	{"remix", true, []string{"remixed"}},
	{"mix", true, []string{"mix"}},
	{"", false, []string{"original", "mix"}},
	{"", false, []string{"stereo", "mix"}},
	{"", false, []string{"mono", "mix"}},
	{"", false, []string{"album", "mix"}},
	{"live", true, []string{"live"}},
	{"cover", true, []string{"cover"}},
	{"slowed", true, []string{"slowed"}},
	{"reverb", true, []string{"reverb"}},
	{"sped up", true, []string{"sped", "up"}},
	{"nightcore", true, []string{"nightcore"}},
	{"8d", true, []string{"8d"}},
	{"reaction", true, []string{"reaction"}},
	{"reacts", true, []string{"reacts"}},
	{"leak", true, []string{"leak"}},
	{"leak", true, []string{"leaked"}},
	{"snippet", true, []string{"snippet"}},
	{"in the booth", true, []string{"in", "the", "booth"}},
	{"type beat", true, []string{"type", "beat"}},
	{"100% accurate", true, []string{"100", "accurate"}},
	{"edit", false, []string{"edit"}},
	{"radio edit", false, []string{"radio", "edit"}},
	{"extended", false, []string{"extended"}},
	{"version", false, []string{"version"}},
}

var longestQualifierPhrase = longestPhraseIn(qualifierLexicon)

func longestPhraseIn(lexicon []qualifierEntry) int {
	longest := 0
	for _, entry := range lexicon {
		longest = max(longest, len(entry.tokens))
	}
	return longest
}

var titleSeparatorRe = regexp.MustCompile(`\s+[\p{Pd}\x{2212}|/]\s+`)

func withoutFeatureCredits(title string) string {
	pieces := titleSeparatorRe.Split(norm.NFKC.String(title), -1)
	for i, piece := range pieces {
		pieces[i] = featuredRe.ReplaceAllString(piece, "")
	}
	return strings.Join(pieces, " ")
}

var qualifierSeparatorRe = regexp.MustCompile(`[._,]+`)

func normalizeToTokens(s string) []string {
	return strings.Fields(textnorm.NormalizeForIdentity(qualifierSeparatorRe.ReplaceAllString(s, " ")))
}

func containsPhrase(tokens, phrase []string) bool {
	for i := 0; i+len(phrase) <= len(tokens); i++ {
		if slices.Equal(tokens[i:i+len(phrase)], phrase) {
			return true
		}
	}
	return false
}

func lexiconEntryAt(tokens []string, pos int) (qualifierEntry, bool) {
	for length := min(longestQualifierPhrase, len(tokens)-pos); length >= 1; length-- {
		phrase := tokens[pos : pos+length]
		for _, entry := range qualifierLexicon {
			if slices.Equal(entry.tokens, phrase) {
				return entry, true
			}
		}
	}
	return qualifierEntry{}, false
}

var yearRe = regexp.MustCompile(`^(?:19|20)\d\d$`)

func isYearQualifiedMix(tokens []string, pos int) bool {
	return tokens[pos] == "mix" && pos > 0 && yearRe.MatchString(tokens[pos-1])
}

func (e qualifierEntry) isBenign() bool {
	return e.label == ""
}

func lexiconMatches(tokens []string) []qualifierEntry {
	var matches []qualifierEntry
	for pos := 0; pos < len(tokens); {
		entry, ok := lexiconEntryAt(tokens, pos)
		if !ok || isYearQualifiedMix(tokens, pos) {
			pos++
			continue
		}
		matches = append(matches, entry)
		pos += len(entry.tokens)
	}
	return matches
}

func appendQualifier(labels []string, label string) []string {
	if slices.Contains(labels, label) {
		return labels
	}
	return append(labels, label)
}

func UnrequestedQualifiers(trackTitle, trackArtist, candidateTitle string) (veto, fallback []string) {
	titleTokens := normalizeToTokens(trackTitle)
	artistTokens := normalizeToTokens(trackArtist)
	for _, entry := range lexiconMatches(normalizeToTokens(withoutFeatureCredits(candidateTitle))) {
		if entry.isBenign() || containsPhrase(titleTokens, entry.tokens) || containsPhrase(artistTokens, entry.tokens) {
			continue
		}
		if entry.veto {
			veto = appendQualifier(veto, entry.label)
			continue
		}
		fallback = appendQualifier(fallback, entry.label)
	}
	return veto, fallback
}

const (
	unrequestedQualifierCost = 1
	unfulfilledQualifierCost = 2

	identityMin   = 60.0
	durationTight = 3
	durationLoose = 15
)

func identityScore(trackTitle, trackArtist, candidateTitle string) float64 {
	combined := textnorm.NormalizeForMatch(trackArtist + " " + trackTitle)
	titleOnly := textnorm.NormalizeForMatch(trackTitle)
	candidateNorm := textnorm.NormalizeForMatch(candidateTitle)

	combinedScore := textnorm.TokenSortRatio(combined, candidateNorm)
	titleOnlyScore := textnorm.TokenSortRatio(titleOnly, candidateNorm)

	titleOnlyScore *= 0.6

	return math.Max(combinedScore, titleOnlyScore)
}

func channelScore(channel string) float64 {
	if isTopicChannel(channel) {
		return 1.0
	}
	if strings.Contains(strings.ToLower(channel), "vevo") {
		return 0.8
	}
	return 0.3
}

func categoryScore(categories []string) float64 {
	for _, c := range categories {
		if c == "Music" {
			return 1.0
		}
	}
	return 0.2
}

func durationScore(expected, actual float64) float64 {
	if expected == 0 || actual == 0 {
		return 0.5
	}
	diff := math.Abs(expected - actual)
	if diff <= durationTight {
		return 1.0
	}
	if diff <= durationLoose {
		return 0.5
	}
	return 0.0
}

func viewScore(viewCount, maxViews int64) float64 {
	if maxViews == 0 {
		return 0.5
	}
	return math.Min(float64(viewCount)/float64(maxViews), 1.0)
}

func metadataRank(c ports.AudioCandidate, expectedDuration float64, maxViews int64) float64 {
	ch := channelScore(c.Channel)
	cat := categoryScore(c.Categories)
	dur := durationScore(expectedDuration, c.Duration)
	views := viewScore(c.ViewCount, maxViews)
	return 0.45*ch + 0.25*dur + 0.20*cat + 0.10*views
}

func isTopicChannel(channel string) bool {
	return strings.HasSuffix(channel, "- Topic")
}

func artistMatchesChannel(trackArtist, channel string) bool {
	artistNorm := strings.ReplaceAll(textnorm.NormalizeForMatch(trackArtist), " ", "")
	if artistNorm == "" {
		return false
	}
	channelNorm := strings.ReplaceAll(textnorm.NormalizeForMatch(channel), " ", "")
	return strings.Contains(channelNorm, artistNorm)
}

type candidateEntry struct {
	ident         float64
	meta          float64
	durationDelta float64
	qualDistance  int
	isFallback    bool
	artistMatch   bool
	featMatch     bool
	candidate     ports.AudioCandidate
}

func durationDelta(expected, actual float64) float64 {
	if expected <= 0 || actual <= 0 {
		return math.MaxFloat64
	}
	return math.Abs(expected - actual)
}

func breakTie(a, b candidateEntry) bool {
	if a.durationDelta != b.durationDelta {
		return a.durationDelta < b.durationDelta
	}
	return a.candidate.URL < b.candidate.URL
}

func lessResolved(a, b candidateEntry) bool {
	return breakTie(a, b)
}

func lessTopic(a, b candidateEntry) bool {
	if a.isFallback != b.isFallback {
		return b.isFallback
	}
	if a.artistMatch != b.artistMatch {
		return a.artistMatch
	}
	if a.featMatch != b.featMatch {
		return a.featMatch
	}
	if a.qualDistance != b.qualDistance {
		return a.qualDistance < b.qualDistance
	}
	if a.ident != b.ident {
		return a.ident > b.ident
	}
	return breakTie(a, b)
}

func lessOther(a, b candidateEntry) bool {
	if a.isFallback != b.isFallback {
		return b.isFallback
	}
	if a.ident != b.ident {
		return a.ident > b.ident
	}
	if a.featMatch != b.featMatch {
		return a.featMatch
	}
	if a.meta != b.meta {
		return a.meta > b.meta
	}
	if a.qualDistance != b.qualDistance {
		return a.qualDistance < b.qualDistance
	}
	return breakTie(a, b)
}

func rankAndCollect(ctx context.Context, track TrackRef, candidates []ports.AudioCandidate) ([]ports.AudioCandidate, []CandidateRejection) {
	if len(candidates) == 0 {
		return nil, nil
	}

	maxViews := maxViewCount(candidates)
	resolved, topic, other, rejected := classifyCandidates(ctx, track, candidates, maxViews)

	sort.SliceStable(resolved, func(i, j int) bool {
		return lessResolved(resolved[i], resolved[j])
	})
	sort.SliceStable(topic, func(i, j int) bool {
		return lessTopic(topic[i], topic[j])
	})
	sort.SliceStable(other, func(i, j int) bool {
		return lessOther(other[i], other[j])
	})

	ranked := make([]ports.AudioCandidate, 0, len(resolved)+len(topic)+len(other))
	for _, bucket := range [][]candidateEntry{resolved, topic, other} {
		for _, e := range bucket {
			ranked = append(ranked, e.candidate)
		}
	}
	return ranked, rejected
}

func maxViewCount(candidates []ports.AudioCandidate) int64 {
	var maxViews int64
	for _, c := range candidates {
		if c.ViewCount > maxViews {
			maxViews = c.ViewCount
		}
	}
	return maxViews
}

func logCandidateEvaluated(ctx context.Context, track TrackRef, c ports.AudioCandidate, ident, meta float64, qualDist int, artMatch, featMatch bool) {
	slog.InfoContext(ctx, "candidate_evaluated",
		"track_id", track.ID,
		"source", c.Source,
		"candidate_title", c.Title,
		"candidate_channel", c.Channel,
		"candidate_duration", c.Duration,
		"candidate_views", c.ViewCount,
		"identity_score", math.Round(ident*10)/10,
		"metadata_rank", math.Round(meta*1000)/1000,
		"qualifier_distance", qualDist,
		"is_topic", isTopicChannel(c.Channel),
		"artist_match", artMatch,
		"feature_match", featMatch,
		"track_artist", track.Artist,
	)
}

func qualifierRejection(c ports.AudioCandidate, veto []string) CandidateRejection {
	return CandidateRejection{
		URL:    c.URL,
		Title:  c.Title,
		Source: c.Source,
		Stage:  RejectionQualifier,
		Reason: "unrequested " + strings.Join(veto, ", "),
	}
}

func unplayableRejection(c ports.AudioCandidate) CandidateRejection {
	return CandidateRejection{
		URL:    c.URL,
		Title:  c.Title,
		Source: c.Source,
		Stage:  unplayableStage(c.Unplayable),
		Reason: "soundcloud " + c.Unplayable,
	}
}

func classifyCandidates(
	ctx context.Context,
	track TrackRef,
	candidates []ports.AudioCandidate,
	maxViews int64,
) (resolved, topic, other []candidateEntry, rejected []CandidateRejection) {
	for _, c := range candidates {
		if c.Unplayable != "" {
			rejected = append(rejected, unplayableRejection(c))
			continue
		}
		ident := identityScore(track.Title, track.Artist, c.Title)
		meta := metadataRank(c, track.Duration, maxViews)
		artMatch := artistMatchesChannel(track.Artist, c.Channel)
		featMatch := featureMatch(track.Title, c.Title)
		qualDist := qualifierDistance(track.Title, c.Title)

		logCandidateEvaluated(ctx, track, c, ident, meta, qualDist, artMatch, featMatch)

		entry := candidateEntry{
			ident:         ident,
			meta:          meta,
			durationDelta: durationDelta(track.Duration, c.Duration),
			qualDistance:  qualDist,
			artistMatch:   artMatch,
			featMatch:     featMatch,
			candidate:     c,
		}

		if c.Resolved {
			resolved = append(resolved, entry)
			continue
		}
		if ident < identityMin && !artMatch {
			rejected = append(rejected, CandidateRejection{
				URL:    c.URL,
				Title:  c.Title,
				Source: c.Source,
				Stage:  RejectionIdentity,
				Reason: fmt.Sprintf("identity %.0f below threshold %.0f", ident, identityMin),
			})
			continue
		}
		veto, fallback := UnrequestedQualifiers(track.Title, track.Artist, c.Title)
		if len(veto) > 0 {
			rejected = append(rejected, qualifierRejection(c, veto))
			continue
		}
		entry.isFallback = len(fallback) > 0
		if isTopicChannel(c.Channel) {
			topic = append(topic, entry)
		} else {
			other = append(other, entry)
		}
	}

	return resolved, topic, other, rejected
}
