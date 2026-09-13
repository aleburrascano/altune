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

func flattenAttr(dst map[string]string, prefix string, a slog.Attr) {
	val := a.Value.Resolve()
	key := a.Key
	if prefix != "" {
		key = prefix + "." + key
	}
	if val.Kind() == slog.KindGroup {
		for _, ga := range val.Group() {
			flattenAttr(dst, key, ga)
		}
		return
	}
	dst[key] = val.String()
}
