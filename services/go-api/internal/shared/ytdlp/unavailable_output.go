package ytdlp

import "strings"

var unavailableMarkers = []string{
	"http error 429",
	"too many requests",
	"rate limit",
	"rate-limit",
	"http error 500",
	"http error 502",
	"http error 503",
	"http error 504",
	"temporary failure in name resolution",
	"name or service not known",
	"failed to resolve",
	"connection refused",
	"connection reset",
	"connection aborted",
	"network is unreachable",
	"no route to host",
	"timed out",
}

func OutputShowsSourceUnavailable(output string) bool {
	lowered := strings.ToLower(output)
	for _, marker := range unavailableMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}
