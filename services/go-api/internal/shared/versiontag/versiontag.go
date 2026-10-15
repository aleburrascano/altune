package versiontag

import (
	"altune/go-api/internal/shared/textnorm"
	"regexp"
	"slices"
	"strings"
)

type Entry struct {
	Label  string
	Veto   bool
	tokens []string
}

func (e Entry) Benign() bool {
	return e.Label == ""
}

var lexicon = []Entry{
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
	{"", false, []string{"album", "version"}},
	{"", false, []string{"explicit", "version"}},
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

var releaseVariants = []Entry{
	{"remaster", false, []string{"remaster"}},
	{"remaster", false, []string{"remastered"}},
	{"re-recorded", false, []string{"re", "recorded"}},
	{"re-recorded", false, []string{"rerecorded"}},
	{"acoustic", false, []string{"acoustic"}},
	{"demo", false, []string{"demo"}},
}

var longestPhrase = longestPhraseIn(lexicon)

func longestPhraseIn(entries []Entry) int {
	longest := 0
	for _, entry := range entries {
		longest = max(longest, len(entry.tokens))
	}
	return longest
}

var FeaturedCredit = regexp.MustCompile(`(?i)\b(?:featuring|feat|ft)\.?\s+([^()\[\]]+)`)

var titleSeparatorRe = regexp.MustCompile(`\s+[\p{Pd}\x{2212}|/]\s+`)

func WithoutFeatureCredits(title string) string {
	pieces := titleSeparatorRe.Split(textnorm.NFKC(title), -1)
	for i, piece := range pieces {
		pieces[i] = FeaturedCredit.ReplaceAllString(piece, "")
	}
	return strings.Join(pieces, " ")
}

var separatorRe = regexp.MustCompile(`[._,]+`)

func Tokens(s string) []string {
	return strings.Fields(textnorm.NormalizeForIdentity(separatorRe.ReplaceAllString(s, " ")))
}

func ContainsPhrase(tokens, phrase []string) bool {
	for i := 0; i+len(phrase) <= len(tokens); i++ {
		if slices.Equal(tokens[i:i+len(phrase)], phrase) {
			return true
		}
	}
	return false
}

func (e Entry) In(tokens []string) bool {
	return ContainsPhrase(tokens, e.tokens)
}

func entryAt(tokens []string, pos int) (Entry, bool) {
	for length := min(longestPhrase, len(tokens)-pos); length >= 1; length-- {
		phrase := tokens[pos : pos+length]
		for _, entry := range lexicon {
			if slices.Equal(entry.tokens, phrase) {
				return entry, true
			}
		}
	}
	return Entry{}, false
}

var yearRe = regexp.MustCompile(`^(?:19|20)\d\d$`)

func isYearQualifiedMix(tokens []string, pos int) bool {
	return tokens[pos] == "mix" && pos > 0 && yearRe.MatchString(tokens[pos-1])
}

func Matches(tokens []string) []Entry {
	var matches []Entry
	for pos := 0; pos < len(tokens); {
		entry, ok := entryAt(tokens, pos)
		if !ok || isYearQualifiedMix(tokens, pos) {
			pos++
			continue
		}
		matches = append(matches, entry)
		pos += len(entry.tokens)
	}
	return matches
}

func AppendLabel(labels []string, label string) []string {
	if slices.Contains(labels, label) {
		return labels
	}
	return append(labels, label)
}

func Labels(title string) []string {
	tokens := Tokens(WithoutFeatureCredits(title))
	var labels []string
	for _, entry := range Matches(tokens) {
		if !entry.Benign() {
			labels = AppendLabel(labels, entry.Label)
		}
	}
	for _, variant := range releaseVariants {
		if variant.In(tokens) {
			labels = AppendLabel(labels, variant.Label)
		}
	}
	slices.Sort(labels)
	return labels
}

func RecordingLabels(title string) []string {
	return slices.DeleteFunc(Labels(title), func(label string) bool { return label == "remaster" })
}

var versionPhrases = slices.Concat(lexicon, releaseVariants)

func versionPhraseAt(tokens []string, pos int) int {
	longest := 0
	for _, entry := range versionPhrases {
		n := len(entry.tokens)
		if n > longest && pos+n <= len(tokens) && slices.Equal(tokens[pos:pos+n], entry.tokens) {
			longest = n
		}
	}
	return longest
}

func WithoutVersionPhrases(tokens []string) []string {
	var kept []string
	for pos := 0; pos < len(tokens); {
		if n := versionPhraseAt(tokens, pos); n > 0 {
			pos += n
			continue
		}
		kept = append(kept, tokens[pos])
		pos++
	}
	return kept
}

func KnownLabels() map[string]bool {
	labels := make(map[string]bool)
	for _, entry := range lexicon {
		labels[entry.Label] = true
	}
	return labels
}
