package main

import (
	"altune/go-api/internal/shared/providerschema"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

type trackersSeen struct {
	path, key string
	query     url.Values
}

func trackersServer(t *testing.T, status int, body string) (*httptest.Server, *[]trackersSeen) {
	t.Helper()
	var seen []trackersSeen
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, trackersSeen{path: r.URL.EscapedPath(), key: r.Header.Get("X-Goog-Api-Key"), query: r.URL.Query()})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func fixedKey() (string, error) { return "test-key", nil }

func TestTrackersGridSendsTheAdapterRequest(t *testing.T) {
	srv, seen := trackersServer(t, http.StatusOK, `{"sheets":[]}`)
	src := newTrackersSource(srv.Client(), srv.URL+"/v4/spreadsheets/", "", fixedKey)
	if _, err := src.Resource(context.Background(), "grid", " sheet123 |'Unreleased'"); err != nil {
		t.Fatal(err)
	}
	got := (*seen)[0]
	want := url.Values{"ranges": {"'Unreleased'"}, "fields": {trackersGridMask}, "prettyPrint": {"false"}}
	if got.path != "/v4/spreadsheets/sheet123" || got.query.Encode() != want.Encode() || got.key != "test-key" {
		t.Fatalf("sent %+v", got)
	}
}

func TestTrackersTabsAsksOnlyForTabTitles(t *testing.T) {
	srv, seen := trackersServer(t, http.StatusOK, `{"sheets":[]}`)
	src := newTrackersSource(srv.Client(), srv.URL+"/", "", fixedKey)
	if _, err := src.Resource(context.Background(), "tabs", "sheet123"); err != nil {
		t.Fatal(err)
	}
	got := (*seen)[0]
	if got.path != "/sheet123" || got.query.Get("fields") != trackersTabsMask || got.query.Has("key") || got.query.Has("ranges") {
		t.Fatalf("sent %+v", got)
	}
}

func TestTrackersGridRejectsAFixtureWithoutARangeBeforeAnyRequest(t *testing.T) {
	srv, seen := trackersServer(t, http.StatusOK, `{}`)
	if _, err := newTrackersSource(srv.Client(), srv.URL+"/", "", fixedKey).Resource(context.Background(), "grid", "sheet123"); err == nil || len(*seen) != 0 {
		t.Fatalf("got %v after %d requests", err, len(*seen))
	}
}

func TestTrackersFailsWithoutAKeyBeforeAnyRequest(t *testing.T) {
	srv, seen := trackersServer(t, http.StatusOK, `{}`)
	noKey := func() (string, error) { return "", errors.New("GOOGLE_SHEETS_API_KEY is not set") }
	if _, err := newTrackersSource(srv.Client(), srv.URL+"/", "", noKey).Resource(context.Background(), "tabs", "s"); err == nil || len(*seen) != 0 {
		t.Fatalf("got %v after %d requests", err, len(*seen))
	}
}

func TestTrackersFailsOnAnErrorStatusWithoutTheKey(t *testing.T) {
	srv, _ := trackersServer(t, http.StatusForbidden, `{}`)
	_, err := newTrackersSource(srv.Client(), srv.URL+"/", "", fixedKey).Resource(context.Background(), "grid", "s|A:A")
	if err == nil || !strings.Contains(err.Error(), "status 403") || strings.Contains(err.Error(), "test-key") {
		t.Fatalf("got %v", err)
	}
}

const testDiscovery = `{
  "resources": {"spreadsheets": {"methods": {"get": {"response": {"$ref": "Spreadsheet"}}}}},
  "schemas": {
    "Spreadsheet": {"type": "object", "properties": {
      "spreadsheetId": {"type": "string"},
      "sheets": {"type": "array", "items": {"$ref": "Sheet"}},
      "locales": {"type": "array", "items": {"type": "string"}},
      "meta": {"type": "object", "additionalProperties": {"type": "string"}},
      "extra": {"type": "any"}
    }},
    "Sheet": {"type": "object", "properties": {
      "index": {"type": "integer"},
      "hidden": {"type": "boolean"},
      "parent": {"$ref": "Spreadsheet"},
      "cell": {"$ref": "Cell"}
    }},
    "Cell": {"type": "object", "properties": {
      "value": {"type": "number"},
      "child": {"$ref": "Cell"}
    }}
  }
}`

func TestTrackersReferenceWalksRefsAndArraysAndStopsCycles(t *testing.T) {
	srv, seen := trackersServer(t, http.StatusOK, testDiscovery)
	src := newTrackersSource(srv.Client(), "", srv.URL, fixedKey)
	got, err := src.Reference(context.Background(), "grid")
	if err != nil {
		t.Fatal(err)
	}
	want := providerschema.Observed{
		"spreadsheetId": "string", "sheets": "array", "locales": "array", "locales[]": "string",
		"meta": "object", "extra": "unknown:any",
		"sheets[].index": "number", "sheets[].hidden": "boolean", "sheets[].parent": "object",
		"sheets[].cell": "object", "sheets[].cell.value": "number", "sheets[].cell.child": "object",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	if _, err := src.Reference(context.Background(), "tabs"); err != nil || len(*seen) != 1 {
		t.Fatalf("want the cached document, got %v after %d fetches", err, len(*seen))
	}
}

func TestTrackersReferenceFails(t *testing.T) {
	missing, _ := trackersServer(t, http.StatusNotFound, `{}`)
	if _, err := newTrackersSource(missing.Client(), "", missing.URL, fixedKey).Reference(context.Background(), "grid"); err == nil {
		t.Fatal("want an error for a missing discovery document")
	}
	empty, _ := trackersServer(t, http.StatusOK, `{"schemas":{}}`)
	if _, err := newTrackersSource(empty.Client(), "", empty.URL, fixedKey).Reference(context.Background(), "grid"); err == nil {
		t.Fatal("want an error when spreadsheets.get has no response schema")
	}
	if _, err := newTrackersSource(empty.Client(), "", empty.URL, fixedKey).Reference(context.Background(), "playlist"); err == nil {
		t.Fatal("want an error for an unknown entity")
	}
}
