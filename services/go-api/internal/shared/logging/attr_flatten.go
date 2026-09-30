package logging

import (
	"fmt"
	"log/slog"
	"unicode/utf8"
)

const ringTextCapBytes = 2048

func capRingText(s string) string {
	if len(s) <= ringTextCapBytes {
		return s
	}
	cut := ringTextCapBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return fmt.Sprintf("%s…[truncated %d]", s[:cut], len(s)-cut)
}

func (h *ringHandler) flattenedAttrs(r slog.Record) map[string]string {
	attrs := make(map[string]string, r.NumAttrs()+len(h.attrs))
	for _, a := range h.attrs {
		flattenAttr(attrs, "", a)
	}
	r.Attrs(func(a slog.Attr) bool {
		flattenAttr(attrs, "", a)
		return true
	})
	return attrs
}

func dottedKey(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}

func flattenAttr(dst map[string]string, prefix string, a slog.Attr) {
	val := a.Value.Resolve()
	key := dottedKey(prefix, a.Key)
	if val.Kind() == slog.KindGroup {
		for _, ga := range val.Group() {
			flattenAttr(dst, key, ga)
		}
		return
	}
	if isSensitiveLeaf(key, val) {
		return
	}
	dst[key] = capRingText(scrubSecrets(val.String()))
}
