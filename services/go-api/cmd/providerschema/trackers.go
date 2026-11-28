package main

import (
	"altune/go-api/cmd/providerschema/internal/providerfields"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	trackersSheetsURL    = "https://sheets.googleapis.com/v4/spreadsheets/"
	trackersDiscoveryURL = "https://sheets.googleapis.com/$discovery/rest?version=v4"
	trackersKeyEnv       = "GOOGLE_SHEETS_API_KEY"
	trackersGridMask     = "sheets.data.rowData.values(formattedValue,hyperlink,textFormatRuns.format.link.uri)"
	trackersTabsMask     = "sheets.properties.title"
)

type trackersEndpoint struct {
	decodedAs
	query func(id string) (sheetID string, values url.Values, err error)
}

var trackersEndpoints = map[string]trackersEndpoint{
	"grid": {"gridResponse", trackersGridQuery},
	"tabs": {"tabsResponse", trackersTabsQuery},
}

func trackersProvider() provider {
	return provider{
		adapter:   adapterRoot + "fansheets",
		decodedAs: decodedTypes(trackersEndpoints),
		source: func(client *http.Client) (source, error) {
			key := func() (string, error) { return envKey(trackersKeyEnv) }
			return newTrackersSource(client, trackersSheetsURL, trackersDiscoveryURL, key), nil
		},
	}
}

type trackersSource struct {
	liveSource
	client    *http.Client
	discovery string
	doc       *discoveryDoc
}

func newTrackersSource(client *http.Client, base, discovery string, key func() (string, error)) *trackersSource {
	fetchers := liveSource{}
	for entity, endpoint := range trackersEndpoints {
		fetchers[entity] = func(ctx context.Context, id string) (json.RawMessage, error) {
			apiKey, err := key()
			if err != nil {
				return nil, err
			}
			sheetID, values, err := endpoint.query(id)
			if err != nil {
				return nil, err
			}
			values.Set("prettyPrint", "false")
			return fetch(ctx, client, request{
				method: http.MethodGet,
				url:    base + url.PathEscape(strings.TrimSpace(sheetID)) + "?" + values.Encode(),
				header: map[string]string{"X-Goog-Api-Key": apiKey},
			})
		}
	}
	return &trackersSource{liveSource: fetchers, client: client, discovery: discovery}
}

func trackersGridQuery(id string) (string, url.Values, error) {
	parts, err := fixtureParts(id, "sheetID", "range")
	if err != nil {
		return "", nil, err
	}
	return parts[0], url.Values{"ranges": {parts[1]}, "fields": {trackersGridMask}}, nil
}

func trackersTabsQuery(sheetID string) (string, url.Values, error) {
	return sheetID, url.Values{"fields": {trackersTabsMask}}, nil
}

func (trackers *trackersSource) Reference(ctx context.Context, entity string) (providerfields.Observed, error) {
	if _, ok := trackersEndpoints[entity]; !ok {
		return nil, fmt.Errorf("trackers has no entity %q", entity)
	}
	doc, err := trackers.document(ctx)
	if err != nil {
		return nil, err
	}
	return doc.response("spreadsheets", "get")
}

func (trackers *trackersSource) document(ctx context.Context) (*discoveryDoc, error) {
	if trackers.doc != nil {
		return trackers.doc, nil
	}
	raw, err := fetch(ctx, trackers.client, request{method: http.MethodGet, url: trackers.discovery})
	if err != nil {
		return nil, fmt.Errorf("sheets discovery document: %w", err)
	}
	var doc discoveryDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("sheets discovery document: %w", err)
	}
	trackers.doc = &doc
	return trackers.doc, nil
}

type discoveryDoc struct {
	Resources map[string]struct {
		Methods map[string]struct {
			Response discoverySchema `json:"response"`
		} `json:"methods"`
	} `json:"resources"`
	Schemas map[string]discoverySchema `json:"schemas"`
}

type discoverySchema struct {
	Type       string                     `json:"type"`
	Ref        string                     `json:"$ref"`
	Properties map[string]discoverySchema `json:"properties"`
	Items      *discoverySchema           `json:"items"`
}

func (doc *discoveryDoc) response(resource, method string) (providerfields.Observed, error) {
	root := doc.Resources[resource].Methods[method].Response.Ref
	if _, ok := doc.Schemas[root]; !ok {
		return nil, fmt.Errorf("sheets discovery document: %s.%s has no response schema", resource, method)
	}
	into := providerfields.Observed{}
	doc.walk(root, "", into, map[string]bool{})
	return into, nil
}

func (doc *discoveryDoc) walk(name, prefix string, into providerfields.Observed, open map[string]bool) {
	if open[name] {
		return
	}
	open[name] = true
	defer delete(open, name)
	for key, property := range doc.Schemas[name].Properties {
		doc.walkProperty(prefix+key, property, into, open)
	}
}

func (doc *discoveryDoc) walkProperty(path string, property discoverySchema, into providerfields.Observed, open map[string]bool) {
	into[path] = discoveryKind(property)
	switch {
	case property.Ref != "":
		doc.walk(property.Ref, path+".", into, open)
	case property.Items != nil && property.Items.Ref != "":
		doc.walk(property.Items.Ref, path+"[].", into, open)
	case property.Items != nil:
		doc.walkProperty(path+"[]", *property.Items, into, open)
	}
}

func discoveryKind(property discoverySchema) string {
	switch property.Type {
	case "":
		if property.Ref != "" {
			return "object"
		}
	case "integer", "number":
		return "number"
	case "string", "boolean", "array", "object":
		return property.Type
	}
	return "unknown:" + property.Type
}
