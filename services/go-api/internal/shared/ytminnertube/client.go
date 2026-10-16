package ytminnertube

import (
	"fmt"
	"maps"
)

const (
	ClientName    = "WEB_REMIX"
	ClientVersion = "1.20220715.04.00"
	APIKey        = "AIzaSyC9XL3ZjWddXya6X74dJoCTL-WEYFDNX30"
	UserAgent     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/70.0.3538.77 Safari/537.36"
	MusicURL      = "https://music.youtube.com"
)

func Body(fields map[string]any) map[string]any {
	body := map[string]any{
		"context": map[string]any{
			"client": map[string]any{"clientName": ClientName, "clientVersion": ClientVersion, "hl": "en", "gl": "US"},
			"user":   map[string]any{"lockedSafetyMode": false},
		},
	}
	maps.Copy(body, fields)
	return body
}

func URL(baseURL, endpoint string) string {
	return fmt.Sprintf("%s/youtubei/v1/%s?key=%s", baseURL, endpoint, APIKey)
}
