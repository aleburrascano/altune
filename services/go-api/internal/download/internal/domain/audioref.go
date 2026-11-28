package domain

import (
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"crypto/sha256"
	"encoding/hex"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

type AudioRefParts struct {
	UserID string
	Artist string
	Album  string
	Title  string
}

func StagedReplace(canonical, attemptID string) string {
	ext := path.Ext(canonical)
	return strings.TrimSuffix(canonical, ext) + ".replace-" + attemptID + ext
}

func TrackScoped(canonical string, trackID ids.TrackId) string {
	ext := path.Ext(canonical)
	return strings.TrimSuffix(canonical, ext) + "." + trackID.String() + ext
}

func NewAudioRef(names AudioRefParts, tempPath string) string {
	return build(names, tempPath, normalizePathComponent)
}

func LegacyAudioRef(names AudioRefParts, tempPath string) string {
	return build(names, tempPath, sanitizePathComponent)
}

func build(names AudioRefParts, tempPath string, segment func(string) string) string {
	artist := segment(names.Artist)
	album := names.Album
	if album == "" {
		album = "Unknown Album"
	}
	album = segment(album)
	title := segment(names.Title)

	ext := filepath.Ext(tempPath)
	if ext == "" {
		ext = ".mp3"
	}
	return strings.Join([]string{names.UserID, artist, album, title + ext}, "/")
}

func normalizePathComponent(s string) string {
	normalized := sanitizePathComponent(music.NormalizeForIdentity(s))
	if normalized != "Unknown" || strings.TrimSpace(s) == "" {
		return normalized
	}
	sum := sha256.Sum256([]byte(s))
	return "Unknown-" + hex.EncodeToString(sum[:4])
}

func sanitizePathComponent(s string) string {
	return capSegmentBytes(sanitizePathComponentUncapped(s))
}

func sanitizePathComponentUncapped(s string) string {
	if s == "" {
		return "Unknown"
	}
	forbidden := `<>:"/\|?*;`
	var b strings.Builder
	for _, r := range s {
		if !strings.ContainsRune(forbidden, r) && (unicode.IsSpace(r) || !unicode.IsControl(r)) {
			b.WriteRune(r)
		}
	}
	result := strings.Join(strings.Fields(b.String()), " ")
	if result == "" {
		return "Unknown"
	}
	if strings.Trim(result, ".") == "" {
		return "Unknown"
	}
	return result
}

const (
	maxSegmentBytes = 120
	segmentHashLen  = 8
)

func capSegmentBytes(s string) string {
	if len(s) <= maxSegmentBytes {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	suffix := "-" + hex.EncodeToString(sum[:])[:segmentHashLen]
	cut := maxSegmentBytes - len(suffix)
	for !utf8.ValidString(s[:cut]) {
		cut--
	}
	return s[:cut] + suffix
}
