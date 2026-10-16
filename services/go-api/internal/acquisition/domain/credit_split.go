package domain

import (
	"altune/go-api/internal/shared/versiontag"
	"regexp"
	"strings"
)

var CreditBrackets = regexp.MustCompile(`(?i)\s*[\(\[](?:feat\.?|ft\.?|featuring|with|bonus)\b[^\)\]]*[\)\]]`)

var creditSeparatorRe = regexp.MustCompile(`(?i)\s*(?:&|,|\b(?:featuring|feat|ft)\b\.?)\s*`)

func SplitCredit(credit string) []string {
	parts := creditSeparatorRe.Split(credit, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func FeaturedCredits(title string) []string {
	m := versiontag.FeaturedCredit.FindStringSubmatch(title)
	if m == nil {
		return nil
	}
	return SplitCredit(m[1])
}
