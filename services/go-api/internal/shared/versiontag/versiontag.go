package versiontag

import (
	"altune/go-api/internal/shared/textnorm"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Tags struct {
	Part   int
	Labels []string
}

func ParseTags(title string) Tags {
	numbered, tokens := withoutVersionNumbers(Tokens(WithoutFeatureCredits(title)))
	return Tags{Part: partNumber(WithoutVersionPhrases(tokens)), Labels: withVersionNumbers(RecordingLabels(title), numbered)}
}

const (
	versionNumberLabelPrefix = "v"
	versionLabel             = "version"
)

var versionWords = []string{"v", "ver", "version"}

var versionNumberRe = regexp.MustCompile(`^v(\d{1,2})$`)

func withoutVersionNumbers(tokens []string) (numbers, rest []string) {
	for pos := 0; pos < len(tokens); pos++ {
		if number := versionNumberAt(tokens, pos); number != "" {
			numbers = append(numbers, number)
			if !versionNumberRe.MatchString(tokens[pos]) {
				pos++
			}
			continue
		}
		rest = append(rest, tokens[pos])
	}
	return numbers, rest
}

func versionNumberAt(tokens []string, pos int) string {
	if match := versionNumberRe.FindStringSubmatch(tokens[pos]); match != nil {
		return versionNumberLabelPrefix + match[1]
	}
	if pos+1 < len(tokens) && slices.Contains(versionWords, tokens[pos]) && isArabicNumber(tokens[pos+1]) {
		return versionNumberLabelPrefix + tokens[pos+1]
	}
	return ""
}

func isArabicNumber(token string) bool {
	n, err := strconv.Atoi(token)
	return err == nil && n >= 1 && len(token) <= 2
}

func withVersionNumbers(labels, numbered []string) []string {
	if len(numbered) == 0 {
		return labels
	}
	labels = slices.DeleteFunc(slices.Clone(labels), func(label string) bool { return label == versionLabel })
	for _, number := range numbered {
		labels = AppendLabel(labels, number)
	}
	slices.Sort(labels)
	return labels
}

func (t Tags) Equal(other Tags) bool {
	return t.Part == other.Part && slices.Equal(t.Labels, other.Labels)
}

const firstPart = 1

var partWords = []string{"pt", "part", "vol", "volume", "chapter", "episode"}

var bareRomanNumerals = map[string]int{"ii": 2, "iii": 3, "iv": 4, "vi": 6, "vii": 7, "viii": 8, "ix": 9}

var keywordedRomanNumerals = map[string]int{"i": 1, "v": 5, "x": 10}

func partNumber(tokens []string) int {
	if len(tokens) < 2 {
		return firstPart
	}
	last := tokens[len(tokens)-1]
	number := bareNumber(last)
	if slices.Contains(partWords, tokens[len(tokens)-2]) {
		number = keywordedNumber(last)
	}
	if number == 0 {
		return firstPart
	}
	return number
}

func keywordedNumber(token string) int {
	if n, ok := keywordedRomanNumerals[token]; ok {
		return n
	}
	return bareNumber(token)
}

func bareNumber(token string) int {
	if n, ok := bareRomanNumerals[token]; ok {
		return n
	}
	if len(token) > 2 {
		return 0
	}
	n, err := strconv.Atoi(token)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

type Entry struct {
	Label  string
	Veto   bool
	tokens []string
}

const remasterLabel = "remaster"

func (e Entry) Benign() bool {
	return e.Label == "" || e.Label == remasterLabel
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
	{remasterLabel, false, []string{"remaster"}},
	{remasterLabel, false, []string{"remastered"}},
	{"re-recorded", true, []string{"re", "recorded"}},
	{"re-recorded", true, []string{"rerecorded"}},
	{"acoustic", true, []string{"acoustic"}},
	{"demo", true, []string{"demo"}},
}

var versionPhrases = slices.Concat(lexicon, releaseVariants)

var longestPhrase = longestPhraseIn(versionPhrases)

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
		for _, entry := range versionPhrases {
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
	return slices.DeleteFunc(Labels(title), func(label string) bool { return label == remasterLabel })
}

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

var rerecordingPhrases = [][]string{
	{"classic", "version"},
	{"re", "recorded"},
	{"rerecorded"},
	{"re", "recording"},
	{"taylors", "version"},
}

func rerecordingPhraseAt(tokens []string, pos int) int {
	for _, phrase := range rerecordingPhrases {
		if pos+len(phrase) <= len(tokens) && slices.Equal(tokens[pos:pos+len(phrase)], phrase) {
			return len(phrase)
		}
	}
	return 0
}

func WithoutRerecordingPhrases(tokens []string) (kept []string, found bool) {
	for pos := 0; pos < len(tokens); {
		if n := rerecordingPhraseAt(tokens, pos); n > 0 {
			found = true
			pos += n
			continue
		}
		kept = append(kept, tokens[pos])
		pos++
	}
	return kept, found
}

func KnownLabels() map[string]bool {
	labels := make(map[string]bool)
	for _, entry := range lexicon {
		labels[entry.Label] = true
	}
	return labels
}
