package songfacts

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const isrcLength = 12

var isrcFormattingRemover = strings.NewReplacer("-", "", " ", "")

func CanonicalISRC(isrc string) string {
	return strings.ToUpper(isrcFormattingRemover.Replace(strings.TrimSpace(isrc)))
}

func IsCanonicalISRC(value string) bool {
	if len(value) != isrcLength {
		return false
	}
	for _, r := range value {
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func NameKey(name string) string {
	var key strings.Builder
	for _, r := range strings.ReplaceAll(strings.ToLower(name), "&", "and") {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			key.WriteRune(r)
		}
	}
	return key.String()
}

var creditSeparatorRe = regexp.MustCompile(`(?i)\s*(?:&|,|;|\s/\s|\s\+\s|\bfeat\.?(?:\s|$)|\bft\.?(?:\s|$)|\bfeaturing\b|\bwith\b|\bx\b|\band\b)\s*`)

func SplitCredit(credit string) []string {
	return creditSeparatorRe.Split(credit, -1)
}

const (
	editionKinds = `original|album|single|mono|stereo|deluxe|bonus(?:\s+track)?`
	editionNouns = `version|edition`
)

var (
	editionLabelRe  = regexp.MustCompile(fmt.Sprintf(`(?i)^\s*(?:%s)(?:\s+(?:%s))?\s*$`, editionKinds, editionNouns))
	editionSuffixRe = regexp.MustCompile(fmt.Sprintf(`(?i)^(.*\S)\s[-–—]\s+([^-–—|]*\b(?:%s|%s)\b[^-–—|]*)$`, editionKinds, editionNouns))
)

func IsEditionLabel(text string) bool {
	return editionLabelRe.MatchString(text)
}

func BracketEditionSuffix(title string) string {
	m := editionSuffixRe.FindStringSubmatch(title)
	if m == nil {
		return title
	}
	return m[1] + " (" + m[2] + ")"
}
