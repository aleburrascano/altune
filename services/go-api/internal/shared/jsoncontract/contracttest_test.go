package jsoncontract_test

import (
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type recordingTB struct {
	testing.TB
	errors []string
	fatal  string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatal = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func assertAgainst(path string, body string) *recordingTB {
	rec := &recordingTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		jsoncontract.AssertMatchesFixture(rec, path, []byte(body))
	}()
	<-done
	return rec
}

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const healthTree = `{"db":"down","detail":{"db_error":"refused","checked_at":"2026-01-01T00:00:00Z"},"checks":[{"name":"db","ok":false}]}`

func TestDrift(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		body    string
		want    []string
	}{
		{"identical trees", healthTree, healthTree, nil},
		{"extra body key", `{"db":"down"}`, `{"db":"down","extra":1}`, nil},
		{"kind mismatch", `{"goroutines":1}`, `{"goroutines":"1"}`, []string{"goroutines: kind number, got string"}},
		{"root kind mismatch", `{}`, `[]`, []string{"$: kind object, got array"}},
		{"renamed key", `{"heap_mb":1}`, `{"heapMB":1}`, []string{"heap_mb: missing"}},
		{"nested missing", `{"detail":{"db_error":"x"}}`, `{"detail":{}}`, []string{"detail.db_error: missing"}},
		{"nested kind mismatch", `{"detail":{"ok":true}}`, `{"detail":{"ok":null}}`, []string{"detail.ok: kind bool, got null"}},
		{"keys reported in order", `{"b":1,"a":1}`, `{}`, []string{"a: missing", "b: missing"}},
		{
			"values vary",
			`{"checked_at":"2026-01-01T00:00:00Z","goroutines":3}`,
			`{"checked_at":"2026-10-08T12:00:00Z","goroutines":41}`,
			nil,
		},
		{"array needs element", `[{"a":1}]`, `[]`, []string{"$: empty array"}},
		{"nested array needs element", `{"jobs":[{"a":1}]}`, `{"jobs":[]}`, []string{"jobs: empty array"}},
		{"both arrays empty", `[]`, `[]`, nil},
		{"array element drift", `[{"a":1}]`, `[{"b":1}]`, []string{"[0].a: missing"}},
		{"invalid fixture", `{`, `{}`, []string{"fixture: invalid JSON: unexpected end of JSON input"}},
		{"invalid body", `{}`, `{`, []string{"body: invalid JSON: unexpected end of JSON input"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := jsoncontract.Drift(json.RawMessage(tc.fixture), json.RawMessage(tc.body))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("drift = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAssertMatchesFixture_AMatchingBodyPasses(t *testing.T) {
	rec := assertAgainst(writeFixture(t, healthTree), healthTree)

	if rec.fatal != "" || len(rec.errors) != 0 {
		t.Fatalf("fatal %q errors %q, want a pass", rec.fatal, rec.errors)
	}
}

func TestAssertMatchesFixture_ReportsEachDriftNamingTheFixture(t *testing.T) {
	rec := assertAgainst(writeFixture(t, `{"a":1,"b":1}`), `{}`)

	want := []string{"fixture.json drifted: a: missing", "fixture.json drifted: b: missing"}
	if rec.fatal != "" || !reflect.DeepEqual(rec.errors, want) {
		t.Fatalf("fatal %q errors %q, want %q", rec.fatal, rec.errors, want)
	}
}

func TestAssertMatchesFixture_AMissingFixtureFailsAndSaysHowToCreateIt(t *testing.T) {
	rec := assertAgainst(filepath.Join(t.TempDir(), "absent.json"), `{}`)

	if !strings.Contains(rec.fatal, "absent.json") || !strings.Contains(rec.fatal, "-update-contract") {
		t.Fatalf("fatal = %q, want it to name the fixture and the -update-contract flag", rec.fatal)
	}
}

func TestAssertMatchesFixture_UpdateRewritesTheFixtureIndented(t *testing.T) {
	if err := flag.Set("update-contract", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flag.Set("update-contract", "false") })
	path := filepath.Join(t.TempDir(), "nested", "fixture.json")

	rec := assertAgainst(path, `{"a":[1]}`)

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read rewritten fixture: %v (fatal %q)", err, rec.fatal)
	}
	if want := "{\n  \"a\": [\n    1\n  ]\n}\n"; string(got) != want || rec.fatal != "" || len(rec.errors) != 0 {
		t.Fatalf("fixture = %q fatal %q errors %q, want %q", got, rec.fatal, rec.errors, want)
	}
}

func TestAssertMatchesFixture_UpdateRefusesABodyThatIsNotJSON(t *testing.T) {
	if err := flag.Set("update-contract", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flag.Set("update-contract", "false") })
	path := filepath.Join(t.TempDir(), "fixture.json")

	rec := assertAgainst(path, `{`)

	if !strings.Contains(rec.fatal, "indent") {
		t.Fatalf("fatal = %q, want the indent failure", rec.fatal)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stat = %v, want no fixture written for a body that is not JSON", err)
	}
}

func TestStrictDrift(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		body    string
		want    []string
	}{
		{"identical", `{"a":1}`, `{"a":2}`, nil},
		{"extra body key", `{"a":1}`, `{"a":1,"b":1}`, []string{"b: unexpected"}},
		{"nested extra key", `{"d":{"a":1}}`, `{"d":{"a":1,"z":1,"y":1}}`, []string{"d.y: unexpected", "d.z: unexpected"}},
		{"extra key in array element", `{"items":[{"a":1}]}`, `{"items":[{"a":1,"b":1}]}`, []string{"items[0].b: unexpected"}},
		{"missing key still reported", `{"a":1}`, `{}`, []string{"a: missing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := jsoncontract.StrictDrift(json.RawMessage(tc.fixture), json.RawMessage(tc.body))
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("drift = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWirePath_NamesAJSONFileUnderTheRepoContractsFolder(t *testing.T) {
	path := jsoncontract.WirePath("tracks_list")

	if !strings.HasSuffix(filepath.ToSlash(path), "/contracts/wire/tracks_list.json") {
		t.Fatalf("path = %q, want it to end in /contracts/wire/tracks_list.json", path)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(path)))
	if _, err := os.Stat(filepath.Join(root, "services", "go-api", "go.mod")); err != nil {
		t.Fatalf("repo root %q does not hold services/go-api: %v", root, err)
	}
}

func TestAssertWire_ReportsAKeyTheFixtureDoesNotHave(t *testing.T) {
	name := "zz_jsoncontract_probe"
	path := jsoncontract.WirePath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	rec := &recordingTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		jsoncontract.AssertWire(rec, name, []byte(`{"a":1,"b":2}`))
	}()
	<-done

	want := []string{name + ".json drifted: b: unexpected"}
	if !reflect.DeepEqual(rec.errors, want) {
		t.Fatalf("errors = %q, want %q", rec.errors, want)
	}
}

func TestAssertWireExact_ReportsAChangedValue(t *testing.T) {
	name := "zz_jsoncontract_exact_probe"
	path := jsoncontract.WirePath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[\n  \"a\",\n  \"b\"\n]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	run := func(body string) *recordingTB {
		rec := &recordingTB{}
		done := make(chan struct{})
		go func() {
			defer close(done)
			jsoncontract.AssertWireExact(rec, name, []byte(body))
		}()
		<-done
		return rec
	}

	if rec := run(`["a", "b"]`); len(rec.errors) != 0 || rec.fatal != "" {
		t.Fatalf("same content reformatted: errors %q fatal %q, want a pass", rec.errors, rec.fatal)
	}
	rec := run(`["a","c"]`)
	if len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "content changed") {
		t.Fatalf("errors = %q, want one content-changed report", rec.errors)
	}
}

func TestAssertWire_UpdateLeavesAFixtureAloneWhenTheShapeStillMatches(t *testing.T) {
	name := "zz_jsoncontract_update_probe"
	path := jsoncontract.WirePath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "{\"a\":\"old\"}\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if err := flag.Set("update-contract", "true"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = flag.Set("update-contract", "false") })

	rec := &recordingTB{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		jsoncontract.AssertWire(rec, name, []byte(`{"a":"new"}`))
	}()
	<-done

	got, err := os.ReadFile(path)
	if err != nil || string(got) != original {
		t.Fatalf("fixture = %q err %v, want it untouched (%q)", got, err, original)
	}
}
