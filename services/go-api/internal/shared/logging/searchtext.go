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

var searchTextKey = func() []byte {
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return k
}()

func SearchTextAttr(text string) slog.Attr {
	mac := hmac.New(sha256.New, searchTextKey)
	mac.Write([]byte(text))
	return slog.Group("search_text",
		slog.Int("len", utf8.RuneCountInString(text)),
		slog.String("fp", hex.EncodeToString(mac.Sum(nil)[:6])),
	)
}

func scrubSearchText(s, text string) string {
	if strings.TrimSpace(text) == "" {
		return s
	}
	for _, form := range []string{text, url.QueryEscape(text), url.PathEscape(text)} {
		s = strings.ReplaceAll(s, form, "[search_text]")
	}
	return s
}

func ScrubSearchErr(err error, text string) string {
	if err == nil {
		return ""
	}
	return scrubSearchText(err.Error(), text)
}
