package main

import (
	"altune/go-api/internal/shared/providerfields"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const appleMusicDocsURL = "https://developer.apple.com/tutorials/data"

type appleMusicEndpoint struct {
	decodedAs
	kind string
}

var appleMusicEndpoints = map[string]appleMusicEndpoint{
	"song":   {"appleMusicSong", "songs"},
	"album":  {"appleMusicAlbum", "albums"},
	"artist": {"appleMusicArtist", "artists"},
}

func appleMusicProvider() provider {
	return provider{
		adapter:   adapterRoot + "applemusic",
		decodedAs: decodedTypes(appleMusicEndpoints),
		source: func(client *http.Client) (source, error) {
			catalog := newAppleMusicCatalog(client, appleMusicSiteURL, appleMusicCatalogURL)
			return newAppleMusicSource(client, catalog, appleMusicDocsURL), nil
		},
	}
}

type appleCatalog interface {
	CatalogResource(ctx context.Context, kind, id string) (json.RawMessage, error)
}

type appleMusicSource struct {
	client  *http.Client
	catalog appleCatalog
	docs    string
	pages   map[string]doccPage
}

func newAppleMusicSource(client *http.Client, catalog appleCatalog, docs string) *appleMusicSource {
	return &appleMusicSource{client: client, catalog: catalog, docs: docs, pages: map[string]doccPage{}}
}

func (apple *appleMusicSource) Resource(ctx context.Context, entity, id string) (json.RawMessage, error) {
	endpoint, ok := appleMusicEndpoints[entity]
	if !ok {
		return nil, fmt.Errorf("apple music has no entity %q", entity)
	}
	return apple.catalog.CatalogResource(ctx, endpoint.kind, id)
}

func (apple *appleMusicSource) Reference(ctx context.Context, entity string) (providerfields.Observed, error) {
	endpoint, ok := appleMusicEndpoints[entity]
	if !ok {
		return nil, fmt.Errorf("apple music has no entity %q", entity)
	}
	into := providerfields.Observed{}
	err := apple.walk(ctx, "/documentation/applemusicapi/"+endpoint.kind, "", into, map[string]bool{})
	return into, err
}

func (apple *appleMusicSource) walk(ctx context.Context, docPath, prefix string, into providerfields.Observed, open map[string]bool) error {
	if open[docPath] {
		return nil
	}
	page, err := apple.page(ctx, docPath)
	if err != nil {
		return err
	}
	open[docPath] = true
	defer delete(open, docPath)
	for _, property := range page.properties() {
		if err := apple.walkProperty(ctx, page, property, prefix, into, open); err != nil {
			return err
		}
	}
	return nil
}

func (apple *appleMusicSource) walkProperty(ctx context.Context, page doccPage, property doccProperty, prefix string, into providerfields.Observed, open map[string]bool) error {
	path, text, link := prefix+property.Name, property.text(), property.link()
	into[path] = doccKind(text, link)
	if strings.HasPrefix(text, "[") {
		path += "[]"
		if link == "" {
			into[path] = doccKind(strings.Trim(text, "[]"), "")
		}
	}
	child := page.References[link].URL
	if link == "" || child == "" {
		return nil
	}
	return apple.walkChild(ctx, child, path+".", into, open)
}

func (apple *appleMusicSource) walkChild(ctx context.Context, docPath, prefix string, into providerfields.Observed, open map[string]bool) error {
	page, err := apple.page(ctx, docPath)
	if err != nil {
		return err
	}
	if !page.isResource() {
		return apple.walk(ctx, docPath, prefix, into, open)
	}
	for _, property := range page.properties() {
		into[prefix+property.Name] = doccKind(property.text(), property.link())
	}
	return nil
}

func (apple *appleMusicSource) page(ctx context.Context, docPath string) (doccPage, error) {
	if page, ok := apple.pages[docPath]; ok {
		return page, nil
	}
	page, err := apple.fetchPage(ctx, docPath)
	if err == nil {
		apple.pages[docPath] = page
	}
	return page, err
}

func (apple *appleMusicSource) fetchPage(ctx context.Context, docPath string) (doccPage, error) {
	body, err := fetch(ctx, apple.client, request{method: http.MethodGet, url: apple.docs + strings.ToLower(docPath) + ".json"})
	if err != nil {
		return doccPage{}, fmt.Errorf("reference %s: %w", docPath, err)
	}
	var page doccPage
	if err := json.Unmarshal(body, &page); err != nil {
		return doccPage{}, fmt.Errorf("reference %s: %w", docPath, err)
	}
	return page, nil
}

type doccPage struct {
	Sections []struct {
		Kind  string         `json:"kind"`
		Items []doccProperty `json:"items"`
	} `json:"primaryContentSections"`
	References map[string]struct {
		URL string `json:"url"`
	} `json:"references"`
}

type doccProperty struct {
	Name string `json:"name"`
	Type []struct {
		Text       string `json:"text"`
		Identifier string `json:"identifier"`
	} `json:"type"`
}

func (page doccPage) properties() []doccProperty {
	var out []doccProperty
	for _, section := range page.Sections {
		if section.Kind == "properties" {
			out = append(out, section.Items...)
		}
	}
	return out
}

func (page doccPage) isResource() bool {
	for _, property := range page.properties() {
		if property.Name == "attributes" {
			return true
		}
	}
	return false
}

func (property doccProperty) text() string {
	var b strings.Builder
	for _, token := range property.Type {
		b.WriteString(token.Text)
	}
	return b.String()
}

func (property doccProperty) link() string {
	for _, token := range property.Type {
		if token.Identifier != "" {
			return token.Identifier
		}
	}
	return ""
}

var doccScalars = map[string]string{"integer": "number", "number": "number", "double": "number", "string": "string", "boolean": "boolean"}

func doccKind(text, link string) string {
	switch {
	case strings.HasPrefix(text, "["):
		return "array"
	case link != "":
		return "object"
	}
	if kind, ok := doccScalars[text]; ok {
		return kind
	}
	return "unknown:" + text
}
