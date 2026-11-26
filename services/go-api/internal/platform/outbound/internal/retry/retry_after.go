package retry

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRetryAfterSeconds = 30

func parseRetryAfter(h string, now time.Time) time.Duration {
	h = strings.TrimSpace(h)
	if h == "" {
		return 0
	}
	if secs, err := strconv.Atoi(h); err == nil {
		return time.Duration(min(max(secs, 0), maxRetryAfterSeconds)) * time.Second
	}
	when, err := http.ParseTime(h)
	if err != nil {
		return 0
	}
	return min(max(when.Sub(now), 0), maxRetryAfterSeconds*time.Second)
}
