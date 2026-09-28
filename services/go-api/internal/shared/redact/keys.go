package redact

import "strings"

var credentialKeyMarkers = []string{
	"secret",
	"password",
	"passwd",
	"credential",
	"api_key",
	"apikey",
	"access_key",
	"secret_key",
	"anon_key",
	"private_key",
}

func IsSecretKey(name string) bool {
	k := strings.ToLower(name)
	if strings.HasSuffix(k, "token") {
		return true
	}
	for _, marker := range credentialKeyMarkers {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}
