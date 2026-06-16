package service

import (
	"math"
	"strconv"
)

type popularityMetric struct {
	key    string
	refMax float64
	parse  func(any) int64
}

var metrics = []popularityMetric{
	{"nb_fan", 100_000_000, parseIntLike},
	{"listeners", 1_000_000_000, parseStringInt},
	{"playback_count", 1_000_000_000, parseIntLike},
}

// NormalizePopularity computes a 0-100 score from raw provider
// popularity metrics in extras, returning the highest across all.
func NormalizePopularity(extras map[string]any) int64 {
	best := int64(0)
	for _, m := range metrics {
		best = maxI64(best, scoreMetric(extras, m))
	}
	return maxI64(best, scoreDeezerRank(extras))
}

func scoreMetric(extras map[string]any, m popularityMetric) int64 {
	v, ok := extras[m.key]
	if !ok {
		return 0
	}
	return logNormalize(m.parse(v), m.refMax)
}

func scoreDeezerRank(extras map[string]any) int64 {
	v, ok := extras["rank"]
	if !ok {
		return 0
	}
	rank := parseIntLike(v)
	if rank <= 0 {
		return 0
	}
	return logNormalize(maxI64(0, 1_000_000-rank), 1_000_000)
}

func logNormalize(count int64, refMax float64) int64 {
	if count <= 0 {
		return 0
	}
	ratio := math.Log10(float64(count)+1) / math.Log10(refMax+1)
	return int64(math.Min(100, ratio*100))
}

func parseIntLike(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func parseStringInt(v any) int64 {
	s, ok := v.(string)
	if !ok {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func maxI64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
