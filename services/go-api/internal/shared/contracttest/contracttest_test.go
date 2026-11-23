package contracttest_test

import (
	"altune/go-api/internal/shared/contracttest"
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
		contracttest.AssertMatchesFixture(rec, path, []byte(body))
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
			got := contracttest.Drift(json.RawMessage(tc.fixture), json.RawMessage(tc.body))
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
