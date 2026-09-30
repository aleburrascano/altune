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
	"jwt",
	"authorization",
	"cookie",
	"signature",
}

var exactSecretKeys = map[string]bool{
	"key":        true,
	"auth":       true,
	"pwd":        true,
	"totp":       true,
	"totpserver": true,
	"client_id":  true,
	"clientid":   true,
}

func IsSecretKey(name string) bool {
	k := strings.ToLower(name)
	if exactSecretKeys[k] || strings.HasSuffix(k, "token") {
		return true
	}
	for _, marker := range credentialKeyMarkers {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}
