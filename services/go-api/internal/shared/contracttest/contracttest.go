package contracttest

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

var updateContract = flag.Bool("update-contract", false, "rewrite contract fixtures")

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
	switch f := fixture.(type) {
	case map[string]any:
		return objectDrift(path, f, body.(map[string]any))
	case []any:
		return arrayDrift(path, f, body.([]any))
	default:
		return nil
	}
}

func objectDrift(path string, fixture, body map[string]any) []string {
	keys := make([]string, 0, len(fixture))
	for key := range fixture {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var drifts []string
	for _, key := range keys {
		child := key
		if path != "" {
			child = path + "." + key
		}
		value, present := body[key]
		if !present {
			drifts = append(drifts, child+": missing")
			continue
		}
		drifts = append(drifts, walkDrift(child, fixture[key], value)...)
	}
	return drifts
}

func arrayDrift(path string, fixture, body []any) []string {
	if len(fixture) == 0 {
		return nil
	}
	if len(body) == 0 {
		return []string{driftLabel(path) + ": empty array"}
	}
	return walkDrift(path+"[0]", fixture[0], body[0])
}

func Drift(fixture, body json.RawMessage) []string {
	var f, b any
	if err := json.Unmarshal(fixture, &f); err != nil {
		return []string{"fixture: invalid JSON: " + err.Error()}
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return []string{"body: invalid JSON: " + err.Error()}
	}
	return walkDrift("", f, b)
}

func AssertMatchesFixture(t testing.TB, path string, body []byte) {
	t.Helper()
	if *updateContract {
		rewriteFixture(t, path, body)
		return
	}
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v (run with -update-contract to create it)", path, err)
	}
	for _, drift := range Drift(fixture, body) {
		t.Errorf("%s drifted: %s", filepath.Base(path), drift)
	}
}

func rewriteFixture(t testing.TB, path string, body []byte) {
	t.Helper()
	var indented bytes.Buffer
	if err := json.Indent(&indented, body, "", "  "); err != nil {
		t.Fatalf("indent %s: %v", path, err)
	}
	indented.WriteByte('\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, indented.Bytes(), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
