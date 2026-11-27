package jsoncontract

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

const wireRoot = "contracts/wire"

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

func walkDrift(path string, fixture, body any, strict bool) []string {
	want, got := jsonKind(fixture), jsonKind(body)
	if want != got {
		return []string{driftLabel(path) + ": kind " + want + ", got " + got}
	}
	switch f := fixture.(type) {
	case map[string]any:
		return objectDrift(path, f, body.(map[string]any), strict)
	case []any:
		return arrayDrift(path, f, body.([]any), strict)
	default:
		return nil
	}
}

func objectDrift(path string, fixture, body map[string]any, strict bool) []string {
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
		drifts = append(drifts, walkDrift(child, fixture[key], value, strict)...)
	}
	if strict {
		drifts = append(drifts, unexpectedKeys(path, fixture, body)...)
	}
	return drifts
}

func unexpectedKeys(path string, fixture, body map[string]any) []string {
	var extra []string
	for key := range body {
		if _, known := fixture[key]; !known {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	drifts := make([]string, 0, len(extra))
	for _, key := range extra {
		child := key
		if path != "" {
			child = path + "." + key
		}
		drifts = append(drifts, child+": unexpected")
	}
	return drifts
}

func arrayDrift(path string, fixture, body []any, strict bool) []string {
	if len(fixture) == 0 {
		return nil
	}
	if len(body) == 0 {
		return []string{driftLabel(path) + ": empty array"}
	}
	return walkDrift(path+"[0]", fixture[0], body[0], strict)
}

func looseDrift(fixture, body json.RawMessage) []string {
	return drift(fixture, body, false)
}

func strictDrift(fixture, body json.RawMessage) []string {
	return drift(fixture, body, true)
}

func drift(fixture, body json.RawMessage, strict bool) []string {
	var f, b any
	if err := json.Unmarshal(fixture, &f); err != nil {
		return []string{"fixture: invalid JSON: " + err.Error()}
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return []string{"body: invalid JSON: " + err.Error()}
	}
	return walkDrift("", f, b, strict)
}

func AssertMatchesFixture(t testing.TB, path string, body []byte) {
	t.Helper()
	assertFixture(t, path, body, looseDrift)
}

func AssertWire(t testing.TB, name string, body []byte) {
	t.Helper()
	assertFixture(t, wirePath(name), body, strictDrift)
}

func AssertWireExact(t testing.TB, name string, body []byte) {
	t.Helper()
	assertFixture(t, wirePath(name), body, exactDrift)
}

func wirePath(name string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join(wireRoot, name+".json")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "..", wireRoot, name+".json")
}

func exactDrift(fixture, body json.RawMessage) []string {
	var want, got bytes.Buffer
	if err := json.Compact(&want, fixture); err != nil {
		return []string{"fixture: invalid JSON: " + err.Error()}
	}
	if err := json.Compact(&got, body); err != nil {
		return []string{"body: invalid JSON: " + err.Error()}
	}
	if want.String() == got.String() {
		return nil
	}
	return []string{"content changed (run -update-contract to accept it)"}
}

func assertFixture(t testing.TB, path string, body []byte, compare func(fixture, body json.RawMessage) []string) {
	t.Helper()
	fixture, err := os.ReadFile(path)
	if *updateContract {
		if err == nil && len(compare(fixture, body)) == 0 {
			return
		}
		rewriteFixture(t, path, body)
		return
	}
	if err != nil {
		t.Fatalf("read fixture %s: %v (run with -update-contract to create it)", path, err)
	}
	for _, drift := range compare(fixture, body) {
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
