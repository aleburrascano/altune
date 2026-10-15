package main

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/songfacts"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
	"net/url"
	"regexp"
	"strings"
)

const (
	categoryWhitespace = "whitespace"
	categorySourceTag  = "source_tag"
	categoryCover      = "cover"

	modeFill    = "fill"
	modeCorrect = "correct"

	maxSourceTagWords = 3
)

type unmatchedTrack struct {
	ID         string
	UserID     string
	Title      string
	Artist     string
	Album      string
	ArtworkURL string
	SourceURL  string
	Version    int
}

type fix struct {
	UserID   string  `json:"user_id"`
	Version  int     `json:"version"`
	Field    string  `json:"field"`
	Mode     string  `json:"mode"`
	OldValue *string `json:"old_value"`
	NewValue *string `json:"new_value"`
}

type finding struct {
	TrackID  string `json:"track_id"`
	Artist   string `json:"artist"`
	Title    string `json:"title"`
	Category string `json:"category"`
	Detail   string `json:"detail"`
	Fix      fix    `json:"fix"`
}

var trailingTagRe = regexp.MustCompile(`^(.*\S)\s*[\[(]([^\[\]()]*)[\])]$`)

var youtubeIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

func collapse(text string) string {
	return strings.Join(strings.Fields(textnorm.NFKC(text)), " ")
}

func isSourceTag(group string) bool {
	if len(versiontag.Labels(group)) > 0 || songfacts.IsEditionLabel(group) || versiontag.FeaturedCredit.MatchString(group) {
		return false
	}
	words := strings.Fields(group)
	residue := strings.Fields(domain.CleanQuery("x " + group))[1:]
	return len(words) <= maxSourceTagWords && len(residue) == 0
}

func withoutSourceTag(title string) string {
	match := trailingTagRe.FindStringSubmatch(title)
	if len(match) != 3 || !isSourceTag(match[2]) {
		return title
	}
	return match[1]
}

func sourceThumbnail(sourceURL string) (string, bool) {
	parsed, err := url.Parse(sourceURL)
	if err != nil {
		return "", false
	}
	host := strings.TrimPrefix(parsed.Hostname(), "music.")
	host = strings.TrimPrefix(host, "www.")
	if host != "youtube.com" {
		return "", false
	}
	id := parsed.Query().Get("v")
	if !youtubeIDRe.MatchString(id) {
		return "", false
	}
	return "https://i.ytimg.com/vi/" + id + "/hqdefault.jpg", true
}

func (t unmatchedTrack) finding(category, detail string, f fix) finding {
	f.UserID, f.Version = t.UserID, t.Version
	return finding{TrackID: t.ID, Artist: t.Artist, Title: t.Title, Category: category, Detail: detail, Fix: f}
}

func (t unmatchedTrack) correction(field, current, category, detail string, tidy func(string) string) []finding {
	cleaned := tidy(current)
	if current == "" || cleaned == "" || cleaned == current {
		return nil
	}
	return []finding{t.finding(category, detail, fix{Field: field, Mode: modeCorrect, OldValue: &current, NewValue: &cleaned})}
}

func (t unmatchedTrack) titleFindings() []finding {
	if collapsed := collapse(t.Title); withoutSourceTag(collapsed) != collapsed {
		return t.correction("title", t.Title, categorySourceTag, "trailing source tag", func(s string) string { return withoutSourceTag(collapse(s)) })
	}
	return t.correction("title", t.Title, categoryWhitespace, "whitespace", collapse)
}

func (t unmatchedTrack) coverFindings() []finding {
	thumbnail, ok := sourceThumbnail(t.SourceURL)
	if strings.TrimSpace(t.ArtworkURL) != "" || !ok {
		return nil
	}
	return []finding{t.finding(categoryCover, "own source thumbnail", fix{Field: "artwork_url", Mode: modeFill, NewValue: &thumbnail})}
}

func hygieneFindings(t unmatchedTrack) []finding {
	var found []finding
	found = append(found, t.titleFindings()...)
	found = append(found, t.correction("artist", t.Artist, categoryWhitespace, "whitespace", collapse)...)
	found = append(found, t.correction("album", t.Album, categoryWhitespace, "whitespace", collapse)...)
	return append(found, t.coverFindings()...)
}
