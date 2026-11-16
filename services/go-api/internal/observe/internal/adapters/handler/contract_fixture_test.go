package handler

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

var updateContract = flag.Bool("update-contract", false, "rewrite contract fixtures")

const contractFixtureDir = "testdata/contract"

func jsonKind(v any) string {
	switch v.(type) {
	case map[string]any:
		return "object"
	case []any:
		return "array"
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "bool"
	default:
		return "null"
	}
}

func driftLabel(path string) string {
	if path == "" {
		return "$"
	}
	return path
}

func walkDrift(path string, fixture, body any) []string {
	want, got := jsonKind(fixture), jsonKind(body)
	if want != got {
		return []string{driftLabel(path) + ": kind " + want + ", got " + got}
	}
	var drifts []string
	switch f := fixture.(type) {
	case map[string]any:
		b := body.(map[string]any)
		keys := make([]string, 0, len(f))
		for key := range f {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child := key
			if path != "" {
				child = path + "." + key
			}
			value, present := b[key]
			if !present {
				drifts = append(drifts, child+": missing")
				continue
			}
			drifts = append(drifts, walkDrift(child, f[key], value)...)
		}
	case []any:
		b := body.([]any)
		if len(f) == 0 {
			return nil
		}
		if len(b) == 0 {
			return []string{driftLabel(path) + ": empty array"}
		}
		drifts = walkDrift(path+"[0]", f[0], b[0])
	}
	return drifts
}

func fixtureDrift(fixture, body json.RawMessage) []string {
	var f, b any
	if err := json.Unmarshal(fixture, &f); err != nil {
		return []string{"fixture: invalid JSON: " + err.Error()}
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return []string{"body: invalid JSON: " + err.Error()}
	}
	return walkDrift("", f, b)
}

func assertMatchesFixture(t *testing.T, name string, body []byte) {
	t.Helper()
	path := filepath.Join(contractFixtureDir, name)
	if *updateContract {
		var indented bytes.Buffer
		if err := json.Indent(&indented, body, "", "  "); err != nil {
			t.Fatalf("indent %s: %v", name, err)
		}
		indented.WriteByte('\n')
		if err := os.MkdirAll(contractFixtureDir, 0o755); err != nil {
			t.Fatalf("create %s: %v", contractFixtureDir, err)
		}
		if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v (run with -update-contract to create it)", path, err)
	}
	for _, drift := range fixtureDrift(fixture, body) {
		t.Errorf("%s drifted: %s", name, drift)
	}
}

func TestFixtureDrift(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(contractFixtureDir, "observe_health.json"))
	if err != nil {
		t.Fatalf("read committed fixture: %v", err)
	}
	cases := []struct {
		name    string
		fixture string
		body    string
		want    []string
	}{
		{"identical trees", string(committed), string(committed), nil},
		{"extra body key", `{"db":"down"}`, `{"db":"down","extra":1}`, nil},
		{"kind mismatch", `{"goroutines":1}`, `{"goroutines":"1"}`, []string{"goroutines: kind number, got string"}},
		{"renamed key", `{"heap_mb":1}`, `{"heapMB":1}`, []string{"heap_mb: missing"}},
		{"nested missing", `{"detail":{"db_error":"x"}}`, `{"detail":{}}`, []string{"detail.db_error: missing"}},
		{
			"values vary",
			`{"checked_at":"2026-01-01T00:00:00Z","goroutines":3}`,
			`{"checked_at":"2026-10-08T12:00:00Z","goroutines":41}`,
			nil,
		},
		{"array needs element", `[{"a":1}]`, `[]`, []string{"$: empty array"}},
		{"both arrays empty", `[]`, `[]`, nil},
		{"array element drift", `[{"a":1}]`, `[{"b":1}]`, []string{"[0].a: missing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := fixtureDrift(json.RawMessage(tc.fixture), json.RawMessage(tc.body))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("drift = %q, want %q", got, tc.want)
			}
		})
	}
}
