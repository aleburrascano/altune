package eval

import (
	"sort"
	"strings"
	"unicode"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

type FailureRecord struct {
	Query  string         `json:"query"`
	Reason string         `json:"reason"`
	Attrs  map[string]any `json:"attrs"`
}

const TokenCountAttr = "token_count"

const ScriptAttr = "script"

const PopBandAttr = "pop_band"

const HasIDAttr = "has_id"

func TokenCount(s string) int {
	return len(strings.Fields(textnorm.NormalizeForMatch(s)))
}

func ScriptClass(raw string) string {
	var latin, other, symbol, letters int
	for _, r := range raw {
		switch {
		case unicode.IsSpace(r) || unicode.IsDigit(r) || unicode.IsPunct(r):
		case unicode.IsLetter(r):
			letters++
			if unicode.Is(unicode.Latin, r) {
				latin++
			} else {
				other++
			}
		default:
			symbol++
		}
	}
	switch {
	case letters == 0:
		return "symbol"
	case other == 0:
		return "latin"
	case latin == 0:
		return "nonlatin"
	default:
		return "mixed"
	}
}

func PopBand(r domain.SearchResult) string {
	p := r.Popularity
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

func HasIdentifier(r domain.SearchResult) bool {
	return r.ISRC != "" || r.MBID != ""
}

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

func QueryAttrs(query string) map[string]any {
	return map[string]any{
		TokenCountAttr: TokenCount(query),
		ScriptAttr:     ScriptClass(query),
	}
}
