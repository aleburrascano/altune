package unreleased

import (
	"altune/go-api/internal/shared/versiontag"
	"regexp"
	"strings"
	"unicode"

	"github.com/agnivade/levenshtein"
	"github.com/mozillazg/go-unidecode"
)

const (
	starMark          = "\ue000"
	minNearKeyLetters = 6
	maxNearDistance   = 2
)

var unreleasedMarkerRe = regexp.MustCompile(`(?i)[(\[]\s*unreleased\s*[)\]]`)

func titleKey(title string) string {
	return keyWithStar(title, "")
}

func wildcardKey(title string) string {
	return keyWithStar(title, starMark)
}

func keyWithStar(title, star string) string {
	cleaned := strings.ReplaceAll(unidecode.Unidecode(unreleasedMarkerRe.ReplaceAllString(title, " ")), "*", star)
	tokens := versiontag.Tokens(versiontag.WithoutFeatureCredits(cleaned))
	core := strings.Join(versiontag.WithoutVersionPhrases(tokens), " ")
	labels := strings.Join(versiontag.RecordingLabels(cleaned), ",")
	return core + "|" + labels
}

func baseKey(title string) string {
	if len(versiontag.RecordingLabels(title)) > 0 {
		return ""
	}
	base, _ := versiontag.BaseTitle(title)
	return titleKey(base)
}

func demoBaseKey(title string) string {
	base, label := versiontag.BaseTitle(title)
	isDemo := label == "Voice Memo" || label == "Demo" || strings.HasPrefix(label, "Demo ")
	if !isDemo || len(versiontag.RecordingLabels(base)) > 0 {
		return ""
	}
	return titleKey(base)
}

func splitKey(key string) (core, labels string) {
	core, labels, _ = strings.Cut(key, "|")
	return core, labels
}

func wildcardMatch(a, b string) bool {
	coreA, labelsA := splitKey(wildcardKey(a))
	coreB, labelsB := splitKey(wildcardKey(b))
	if labelsA != labelsB || !strings.Contains(coreA+coreB, starMark) {
		return false
	}
	pattern, text := coreA, coreB
	if !strings.Contains(pattern, starMark) {
		pattern, text = coreB, coreA
	}
	quoted := strings.ReplaceAll(regexp.QuoteMeta(pattern), starMark, `\p{L}+`)
	return regexp.MustCompile("^" + quoted + "$").MatchString(text)
}

func nearMatch(a, b string) bool {
	coreA, labelsA := splitKey(titleKey(a))
	coreB, labelsB := splitKey(titleKey(b))
	if labelsA != labelsB || letterCount(coreA) < minNearKeyLetters || letterCount(coreB) < minNearKeyLetters {
		return false
	}
	return levenshtein.ComputeDistance(coreA, coreB) <= maxNearDistance
}

func looseBase(title string) (base, versionLabel string, ok bool) {
	base, versionLabel = versiontag.BaseTitle(title)
	return base, versionLabel, len(versiontag.RecordingLabels(title)) == 0
}

func letterCount(s string) int {
	count := 0
	for _, r := range s {
		if unicode.IsLetter(r) {
			count++
		}
	}
	return count
}
