package logging

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/url"
	"strings"
	"unicode/utf8"
)

// searchTextKey keys a per-process HMAC for search-text fingerprints. It is
// random and never persisted, so a fingerprint correlates log lines for the
// same text within one process lifetime but cannot be reversed by hashing a
// dictionary of likely queries, and becomes meaningless after a restart. That
// keeps user search text — which clear-history promises to erase — out of
// stdout logs, whose retention and audience the deletion path cannot reach.
var searchTextKey = func() []byte {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return k
}()

// SearchTextAttr returns a loggable stand-in for user search text: its length
// in runes and a short, process-scoped, non-reversible fingerprint. Log this
// instead of the raw text.
func SearchTextAttr(text string) slog.Attr {
	mac := hmac.New(sha256.New, searchTextKey)
	mac.Write([]byte(text))
	return slog.Group("search_text",
		slog.Int("len", utf8.RuneCountInString(text)),
		slog.String("fp", hex.EncodeToString(mac.Sum(nil)[:6])),
	)
}

// ScrubSearchText removes every raw, query-escaped, and path-escaped occurrence
// of text from s, so an error that embeds a request URL can be logged without
// leaking the search text it carried.
func ScrubSearchText(s, text string) string {
	if strings.TrimSpace(text) == "" {
		return s
	}
	for _, form := range []string{text, url.QueryEscape(text), url.PathEscape(text)} {
		s = strings.ReplaceAll(s, form, "[search_text]")
	}
	return s
}

// ScrubSearchErr is ScrubSearchText applied to an error's message; nil stays "".
func ScrubSearchErr(err error, text string) string {
	if err == nil {
		return ""
	}
	return ScrubSearchText(err.Error(), text)
}
