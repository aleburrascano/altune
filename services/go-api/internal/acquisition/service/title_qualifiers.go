package service

import (
	"altune/go-api/internal/shared/textnorm"
	"regexp"
	"slices"
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
