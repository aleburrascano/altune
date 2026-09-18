package logging

import "log/slog"

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

// dottedKey is the key a leaf is judged and displayed under. Both walks over
// attrs — this one and the redaction filter — build it here, so neither can
// drift into judging a nested leaf by a different key than the other.
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
	// Checked per leaf with the full dotted key, so secrets nested in a group
	// or bound via logger.With are caught, not just top-level record attrs.
	if isSensitiveLeaf(key, val) {
		return
	}
	dst[key] = scrubSecrets(val.String())
}
