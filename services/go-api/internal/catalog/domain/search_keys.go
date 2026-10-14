package domain

import (
	"altune/go-api/internal/shared/textnorm"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

type SearchKeys struct {
	Artist             string
	Title              string
	Qualifiers         []string
	Variant            bool
	ArtistIsChannel    bool
	ArtistFromUploader bool
}

type qualifierRule struct {
	name    string
	re      *regexp.Regexp
	variant bool
}

var qualifierRules = []qualifierRule{
	{"slowed", regexp.MustCompile(`(?i)\bslowed\b|慢速|スロー`), true},
	{"reverb", regexp.MustCompile(`(?i)\breverb\b|リバーブ`), true},
	{"sped up", regexp.MustCompile(`(?i)\b(?:sped|speed(?:ed)?)[\s-]*up\b|加速|倍速`), true},
	{"nightcore", regexp.MustCompile(`(?i)\bnightcore\b|ナイトコア`), true},
	{"remix", regexp.MustCompile(`(?i)\b(?:remix(?:ed)?|rmx)\b|リミックス|混音|리믹스`), true},
	{"live", regexp.MustCompile(`(?i)^\W*live\b|\blive\s*$|\blive\s+(?:at|from|in|on|version|session|performance|video)\b|ライブ|现场|現場|라이브`), true},
	{"extended", regexp.MustCompile(`(?i)\bextended\b`), true},
	{"acoustic", regexp.MustCompile(`(?i)\bacoustic\b|アコースティック`), true},
	{"unplugged", regexp.MustCompile(`(?i)\bunplugged\b`), true},
	{"instrumental", regexp.MustCompile(`(?i)\binstrumental\b|伴奏|インスト`), true},
	{"demo", regexp.MustCompile(`(?i)\bdemo\b`), true},
	{"cover", regexp.MustCompile(`(?i)^(?:\w+\s+){0,3}cover(?:\s+version)?\s*$`), true},
	{"mashup", regexp.MustCompile(`(?i)\bmash-?up\b`), true},
	{"bass boosted", regexp.MustCompile(`(?i)\bbass[\s-]*boost(?:ed)?\b`), true},
	{"8d audio", regexp.MustCompile(`(?i)\b8d\b`), true},
	{"radio edit", regexp.MustCompile(`(?i)\bradio\s+(?:edit|version|mix)\b`), true},
	{"edit", regexp.MustCompile(`(?i)\bedit\b`), true},
	{"remaster", regexp.MustCompile(`(?i)\bre-?master(?:ed)?\b`), false},
	{"clean", regexp.MustCompile(`(?i)\bclean\b`), false},
	{"explicit", regexp.MustCompile(`(?i)\bexplicit\b`), false},
}

var (
	groupRe       = regexp.MustCompile(`[(\[{（【][^)\]}）】]*[)\]}）】]`)
	separatorRe   = regexp.MustCompile(`\s+[-–—|]\s+`)
	dashSuffixRe  = regexp.MustCompile(`\s[-–—]\s+([^-–—|]+)$`)
	creditGroupRe = regexp.MustCompile(`(?i)^\s*(?:feat|ft|featuring|with)\b`)
	trailingFeat  = regexp.MustCompile(`(?i)\s+(?:feat|ft|featuring)\.?\s+.+$`)
	trailingHDRe  = regexp.MustCompile(`\s+(?:HD|HQ)\s*$`)
	videoExtRe    = regexp.MustCompile(`(?i)\.(?:avi|wmv|mpg|mpeg|flv)$`)
	spacesRe      = regexp.MustCompile(`\s+`)
	trimEdgesRe   = regexp.MustCompile(`^[/,:;~\s"-]+|[/,:;~\s"-]+$`)
	wrappedQuote  = regexp.MustCompile(`^(['"“‘])(.{2,})(['"”’])$`)
	dashVideoRe   = regexp.MustCompile(`(?i)\s-\s(?:of+icial\s*)?(?:music\s*)?(?:video|audio)$`)
	channelTopic  = regexp.MustCompile(`(?i)\s*-\s*topic$`)
	channelVevo   = regexp.MustCompile(`(?i)\s*vevo$`)
	channelOffice = regexp.MustCompile(`(?i)\s+official$|^official\s+`)
)

var noiseGroupRes = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(?:\s|official|of+icial|lyrics?|music|hd|hq|audio|video|visuali[sz]er|stream|track|full|album|clip|4k|1080p|720p)+$`),
	regexp.MustCompile(`(?i)^(?:prod\.|prod\s|produced\s)`),
	regexp.MustCompile(`(?i)^\s*(?:album|single|mono|stereo|deluxe|bonus track)(?:\s+(?:edition|version))?\s*$`),
	regexp.MustCompile(`^\s*\d{4}\s*$`),
	regexp.MustCompile(`(?i)re-?issue|^\s*expanded|^\s*re-?recorded\s*$|^\d+th\s+anniversary`),
	regexp.MustCompile(`(?i)album\s+track\s*$`),
}

func CleanSearchKeys(title, artist, uploader string) SearchKeys {
	original := strings.TrimSpace(textnorm.NFKC(title))
	storedArtist := channelStripped(dropDuplicateGroups(artist))
	channelName := channelStripped(dropDuplicateGroups(uploader))
	keys := initialKeys(artist, storedArtist, channelName)

	found := map[string]bool{}
	work := splitCredit(dropSymbols(original), found, storedArtist, channelName)
	keys.Title = stripDecorations(work, found)
	if keys.Title == "" {
		keys.Title = tidy(original)
	}
	keys.Qualifiers, keys.Variant = orderedQualifiers(found)
	return keys
}

func initialKeys(rawArtist, storedArtist, channelName string) SearchKeys {
	keys := SearchKeys{Artist: storedArtist, ArtistIsChannel: looksLikeChannel(rawArtist)}
	if storedArtist == "" {
		keys.Artist = channelName
		keys.ArtistFromUploader = channelName != ""
	}
	return keys
}

func stripDecorations(work string, found map[string]bool) string {
	work = stripDecorationGroups(work, found)
	work = stripQualifierSuffixes(work, found)
	work = dashVideoRe.ReplaceAllString(work, "")
	work = videoExtRe.ReplaceAllString(work, "")
	work = trailingFeat.ReplaceAllString(work, "")
	work = trailingHDRe.ReplaceAllString(work, "")
	return tidy(work)
}

func orderedQualifiers(found map[string]bool) (names []string, variant bool) {
	for _, rule := range qualifierRules {
		if found[rule.name] {
			names = append(names, rule.name)
			variant = variant || rule.variant
		}
	}
	slices.Sort(names)
	return names, variant
}

func stripDecorationGroups(work string, found map[string]bool) string {
	return rewriteGroups(work, func(prefix, group string) bool {
		inner := groupInner(group)
		matched := matchQualifiers(inner)
		for _, name := range matched {
			found[name] = true
		}
		return len(matched) == 0 && !isDroppableGroup(prefix, inner)
	})
}

func stripQualifierSuffixes(work string, found map[string]bool) string {
	for {
		m := dashSuffixRe.FindStringSubmatch(work)
		if len(m) < 2 || len(strings.Fields(m[1])) > 5 {
			return work
		}
		matched := matchQualifiers(m[1])
		if len(matched) == 0 {
			return work
		}
		for _, name := range matched {
			found[name] = true
		}
		work = strings.TrimSuffix(work, m[0])
	}
}

func splitCredit(title string, found map[string]bool, names ...string) string {
	locs := separatorRe.FindAllStringIndex(title, -1)
	if len(locs) == 0 {
		return title
	}
	first, last := locs[0], locs[len(locs)-1]
	if isArtistName(title[:first[0]], names) {
		noteGroupQualifiers(title[:first[0]], found)
		return title[first[1]:]
	}
	if isArtistName(title[last[1]:], names) {
		noteGroupQualifiers(title[last[1]:], found)
		return title[:last[0]]
	}
	return title
}

func noteGroupQualifiers(dropped string, found map[string]bool) {
	for _, group := range groupRe.FindAllString(dropped, -1) {
		for _, name := range matchQualifiers(groupInner(group)) {
			found[name] = true
		}
	}
}

func isArtistName(side string, names []string) bool {
	side = dropDuplicateGroups(side)
	for _, name := range names {
		if sameName(side, name) {
			return true
		}
	}
	return false
}

func sameName(a, b string) bool {
	na := textnorm.NormalizeForMatch(a)
	return na != "" && na == textnorm.NormalizeForMatch(b)
}

func matchQualifiers(text string) []string {
	var names []string
	for _, rule := range qualifierRules {
		if rule.re.MatchString(text) {
			names = append(names, rule.name)
		}
	}
	if slices.Contains(names, "radio edit") {
		names = slices.DeleteFunc(names, func(name string) bool { return name == "edit" })
	}
	return names
}

func isDroppableGroup(prefix, inner string) bool {
	if creditGroupRe.MatchString(inner) || (isNonLatinText(inner) && endsWithLatin(prefix)) {
		return true
	}
	for _, re := range noiseGroupRes {
		if re.MatchString(inner) {
			return true
		}
	}
	return false
}

func isNonLatinText(text string) bool {
	foreign := false
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana):
			foreign = true
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			return false
		}
	}
	return foreign
}

func dropDuplicateGroups(s string) string {
	return tidy(rewriteGroups(s, func(prefix, group string) bool {
		return !isNonLatinText(groupInner(group)) || !endsWithLatin(prefix)
	}))
}

func endsWithLatin(prefix string) bool {
	runes := []rune(strings.TrimSpace(prefix))
	if len(runes) == 0 {
		return false
	}
	last := runes[len(runes)-1]
	return unicode.Is(unicode.Latin, last) || unicode.IsDigit(last)
}

func rewriteGroups(s string, keep func(prefix, group string) bool) string {
	var b strings.Builder
	cursor := 0
	for _, loc := range groupRe.FindAllStringIndex(s, -1) {
		b.WriteString(s[cursor:loc[0]])
		if group := s[loc[0]:loc[1]]; keep(s[:loc[0]], group) {
			b.WriteString(group)
		}
		cursor = loc[1]
	}
	b.WriteString(s[cursor:])
	return b.String()
}

func groupInner(group string) string {
	runes := []rune(group)
	return string(runes[1 : len(runes)-1])
}

func dropSymbols(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.So, r) {
			return ' '
		}
		return r
	}, s)
}

func looksLikeChannel(artist string) bool {
	return channelTopic.MatchString(artist) || channelVevo.MatchString(artist) || channelOffice.MatchString(artist)
}

func channelStripped(name string) string {
	name = channelTopic.ReplaceAllString(name, "")
	name = channelVevo.ReplaceAllString(name, "")
	name = channelOffice.ReplaceAllString(name, "")
	return tidy(name)
}

func tidy(s string) string {
	s = spacesRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(trimEdgesRe.ReplaceAllString(strings.TrimSpace(s), ""))
	if m := wrappedQuote.FindStringSubmatch(s); m != nil {
		s = strings.TrimSpace(m[2])
	}
	return s
}
