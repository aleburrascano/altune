package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type WikidataMbidResolver struct {
	client *http.Client
}

func NewWikidataMbidResolver(client *http.Client) *WikidataMbidResolver {
	return &WikidataMbidResolver{client: client}
}

func (r *WikidataMbidResolver) Resolve(ctx context.Context, inputURL string) (string, error) {
	deezerID := extractDeezerArtistID(inputURL)
	if deezerID == "" {
		return "", nil
	}

	query := fmt.Sprintf(`SELECT ?mbid WHERE { ?item wdt:P2722 "%s" ; wdt:P434 ?mbid . } LIMIT 1`, deezerID)
	return r.runSparql(ctx, query, "mbid")
}

func (r *WikidataMbidResolver) runSparql(ctx context.Context, query, field string) (string, error) {
	u := "https://query.wikidata.org/sparql"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", nil
	}

	q := req.URL.Query()
	q.Set("query", query)
	q.Set("format", "json")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/sparql-results+json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 || resp.StatusCode != 200 {
		return "", nil
	}

	var data struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", nil
	}

	if len(data.Results.Bindings) > 0 {
		if val, ok := data.Results.Bindings[0][field]; ok && val.Value != "" {
			return val.Value, nil
		}
	}

	return "", nil
}

func extractDeezerArtistID(inputURL string) string {
	parsed, err := url.Parse(inputURL)
	if err != nil {
		return ""
	}
	if parsed.Host != "www.deezer.com" && parsed.Host != "deezer.com" {
		return ""
	}
	parts := splitPath(parsed.Path)
	for i, p := range parts {
		if p == "artist" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
