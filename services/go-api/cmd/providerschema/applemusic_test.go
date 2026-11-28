package main

import (
	"altune/go-api/cmd/providerschema/internal/providerfields"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func prop(name, text, link string) string {
	if link == "" {
		return `{"name":"` + name + `","type":[{"kind":"text","text":"` + text + `"}]}`
	}
	return `{"name":"` + name + `","type":[{"kind":"text","text":"` + text + `","identifier":"` + link + `"}]}`
}

func page(refs string, props ...string) string {
	items := ""
	for i, p := range props {
		if i > 0 {
			items += ","
		}
		items += p
	}
	return `{"primaryContentSections":[{"kind":"properties","items":[` + items + `]}],"references":{` + refs + `}}`
}

func docsServer(t *testing.T) *httptest.Server {
	t.Helper()
	pages := map[string]string{
		"/documentation/applemusicapi/songs.json": page(`"A":{"url":"/documentation/applemusicapi/songs/attributes"},"R":{"url":"/documentation/applemusicapi/songs/rel"}`,
			prop("id", "string", ""), prop("attributes", "Songs.Attributes", "A"), prop("relationships", "Songs.Relationships", "R")),
		"/documentation/applemusicapi/songs/attributes.json": page(`"P":{"url":"/documentation/applemusicapi/preview"}`,
			prop("durationInMillis", "integer", ""), prop("genreNames", "[string]", ""), prop("previews", "[Preview]", "P"), prop("odd", "Date", "")),
		"/documentation/applemusicapi/preview.json": page("", prop("url", "string", "")),
		"/documentation/applemusicapi/songs/rel.json": page(`"S":{"url":"/documentation/applemusicapi/songs"}`,
			prop("data", "[Songs]", "S")),
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAppleReferenceWalksNestedTypesAndStopsAtResources(t *testing.T) {
	srv := docsServer(t)
	got, err := newAppleMusicSource(srv.Client(), nil, srv.URL).Reference(context.Background(), "song")
	if err != nil {
		t.Fatal(err)
	}
	want := providerfields.Observed{
		"id": "string", "attributes": "object",
		"attributes.durationInMillis": "number",
		"attributes.genreNames":       "array", "attributes.genreNames[]": "string",
		"attributes.previews": "array", "attributes.previews[].url": "string",
		"attributes.odd": "unknown:Date",
		"relationships":  "object", "relationships.data": "array",
		"relationships.data[].id": "string", "relationships.data[].attributes": "object",
		"relationships.data[].relationships": "object",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestAppleReferenceFailsWhenAPageIsMissing(t *testing.T) {
	srv := docsServer(t)
	if _, err := newAppleMusicSource(srv.Client(), nil, srv.URL+"/nowhere").Reference(context.Background(), "song"); err == nil {
		t.Fatal("want an error")
	}
}

func TestAppleSourceRejectsAnUnknownEntity(t *testing.T) {
	s := newAppleMusicSource(http.DefaultClient, nil, "")
	if _, err := s.Reference(context.Background(), "playlist"); err == nil {
		t.Fatal("want an error from Reference")
	}
	if _, err := s.Resource(context.Background(), "playlist", "1"); err == nil {
		t.Fatal("want an error from Resource")
	}
}

type fakeCatalog struct{ kind, id string }

func (f *fakeCatalog) CatalogResource(_ context.Context, kind, id string) (json.RawMessage, error) {
	f.kind, f.id = kind, id
	return json.RawMessage(`{}`), nil
}

func TestAppleResourceAsksTheCatalogForThePluralKind(t *testing.T) {
	catalog := &fakeCatalog{}
	if _, err := newAppleMusicSource(http.DefaultClient, catalog, "").Resource(context.Background(), "album", "9"); err != nil {
		t.Fatal(err)
	}
	if catalog.kind != "albums" || catalog.id != "9" {
		t.Fatalf("asked for %s/%s", catalog.kind, catalog.id)
	}
}
