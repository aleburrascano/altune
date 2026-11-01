package unreleased

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/versiontag"
	"regexp"
	"strings"
	"unicode"
)

const (
	starMark      = "ǂ"
	minKeyLetters = 6
)

func titleKey(title string) string {
	return domain.TitleKey(title)
}

func wildcardKey(title string) string {
	return domain.TitleKeyWithStar(title, starMark)
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
	pattern, text := coreA, coreB
	if !strings.Contains(pattern, starMark) {
		pattern, text = coreB, coreA
	}
	if labelsA != labelsB || !strings.Contains(pattern, starMark) || strings.Contains(text, starMark) {
		return false
	}
	if letterCount(strings.ReplaceAll(pattern, starMark, "")) < minKeyLetters || letterCount(text) < minKeyLetters {
		return false
	}
	quoted := strings.ReplaceAll(regexp.QuoteMeta(pattern), starMark, `\p{L}+`)
	return regexp.MustCompile("^" + quoted + "$").MatchString(text)
}

func nearMatch(a, b string) bool {
	coreA, labelsA := splitKey(titleKey(a))
	coreB, labelsB := splitKey(titleKey(b))
	if labelsA != labelsB || letterCount(coreA) < minKeyLetters || letterCount(coreB) < minKeyLetters {
		return false
	}
	runesA, runesB := []rune(coreA), []rune(coreB)
	if len(runesA) != len(runesB) {
		return false
	}
	differing := 0
	for i := range runesA {
		if runesA[i] != runesB[i] {
			differing++
		}
	}
	return differing == 1
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
