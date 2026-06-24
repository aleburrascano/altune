package service

// Eval substrate — the attributed failure log (artifact D, plan 2026-06-24-001).
//
// The failure log, not the slices, is the diagnostic system of record. Every
// harness miss is recorded with its full cheap attribute bag so failure modes
// are an OUTPUT read at investigation time — re-groupable by any axis, including
// ones nobody anticipated today. We never maintain a failure-mode taxonomy; we
// keep the failures, fully described.
//
// The four mechanical feature functions below (token count, script class, plus
// popularity-band / has-identifier computed from a result) are disposable
// convenience slices printed on top of the log — deletable without losing power,
// because the raw records can always be re-grouped. They are admitted ONLY
// because a computer derives them deterministically; the moment a "feature"
// needs human judgment ("is this query ambiguous?") it rots, so it is banned.
// Ambiguity then surfaces emergently as, e.g., single-token + low-pop being the
// band that is red.

import (
	"sort"
	"strings"
	"unicode"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// FailureRecord is one harness miss plus its cheap attribute bag. Attrs vary by
// harness — each populates whatever features are free for it (a merge miss has
// popularity and has-id; a zero-result query has neither). The bag is the point:
// group it however you like later.
type FailureRecord struct {
	Query  string         `json:"query"`
	Reason string         `json:"reason"`
	Attrs  map[string]any `json:"attrs"`
}

// TokenCountAttr is the canonical "token_count" key, set by every harness that
// has a query string so the single-token band is always sliceable.
const TokenCountAttr = "token_count"

// ScriptAttr is the canonical "script" key (latin | nonlatin | symbol | mixed).
const ScriptAttr = "script"

// PopBandAttr is the canonical "pop_band" key (high | mid | low | none).
const PopBandAttr = "pop_band"

// HasIDAttr is the canonical "has_id" key (true when an isrc/mbid is present).
const HasIDAttr = "has_id"

// TokenCount counts tokens the way the pipeline does — fields of the canonical
// normalization. "single-token" (the known hard case) is TokenCount == 1.
func TokenCount(s string) int {
	return len(strings.Fields(textnorm.NormalizeForMatch(s)))
}

// ScriptClass labels a string's writing system from the RAW text (before
// normalization, which strips diacritics and non-word runes). Used to surface
// the matcher's known non-Latin / symbol weak spots — the "¥$" band.
func ScriptClass(raw string) string {
	var latin, other, symbol, letters int
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r) || unicode.IsDigit(r) || unicode.IsPunct(r):
			// neutral — ignored for classification
		case unicode.IsLetter(r):
			letters++
			if unicode.Is(unicode.Latin, r) {
				latin++
			} else {
				other++
			}
		default:
			symbol++ // currency/math/emoji etc. — the "¥$" band
		}
	}
	switch {
	case letters == 0:
		return "symbol" // symbol-only or nothing classifiable
	case other == 0:
		return "latin"
	case latin == 0:
		return "nonlatin"
	default:
		return "mixed"
	}
}

// PopBand buckets a result's popularity into a coarse, mechanical band. The cut
// points are display buckets (like a page size), not query-fit ranking
// constants — they only group the failure log, they never touch ranking.
func PopBand(r domain.SearchResult) string {
	p := popularityOf(r)
	switch {
	case p <= 0:
		return "none"
	case p < 30:
		return "low"
	case p < 70:
		return "mid"
	default:
		return "high"
	}
}

// HasIdentifier reports whether a result carries an isrc or mbid — the merge
// oracle's slice axis and a free attribute everywhere a result is in hand.
func HasIdentifier(r domain.SearchResult) bool {
	return stringExtra(r, "isrc") != "" || stringExtra(r, "mbid") != ""
}

// SliceFailures groups records by one attribute key into value→count. Values are
// stringified so any attr type slices uniformly. Records missing the key fall
// into "(unset)".
func SliceFailures(records []FailureRecord, attrKey string) map[string]int {
	out := map[string]int{}
	for _, r := range records {
		v, ok := r.Attrs[attrKey]
		key := "(unset)"
		if ok {
			key = stringifyAttr(v)
		}
		out[key]++
	}
	return out
}

// SliceFailuresByPair groups by two attribute keys at once (e.g. token_count ×
// pop_band) — the joint band where ambiguity emerges. Key is "a|b".
func SliceFailuresByPair(records []FailureRecord, keyA, keyB string) map[string]int {
	out := map[string]int{}
	for _, r := range records {
		a, b := "(unset)", "(unset)"
		if v, ok := r.Attrs[keyA]; ok {
			a = stringifyAttr(v)
		}
		if v, ok := r.Attrs[keyB]; ok {
			b = stringifyAttr(v)
		}
		out[a+"|"+b]++
	}
	return out
}

// TopBuckets renders a slice map as count-descending "key=count" pairs, capped
// at n, for the default report view. Ties break on key for stable output.
func TopBuckets(slice map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(slice))
	for k, v := range slice {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v
		}
		return pairs[i].k < pairs[j].k
	})
	if n > 0 && len(pairs) > n {
		pairs = pairs[:n]
	}
	out := make([]string, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, p.k+"="+itoa(p.v))
	}
	return out
}

func stringifyAttr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case int:
		return itoa(t)
	default:
		return "?"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// QueryAttrs is the standard cheap attribute bag for a query-keyed failure —
// token count + script class. Harnesses with a result in hand add PopBand /
// HasIdentifier on top.
func QueryAttrs(query string) map[string]any {
	return map[string]any{
		TokenCountAttr: TokenCount(query),
		ScriptAttr:     ScriptClass(query),
	}
}
