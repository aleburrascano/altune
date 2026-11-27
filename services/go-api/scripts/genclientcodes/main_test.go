package main

import (
	"altune/go-api/internal/shared/apierror"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generate(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr strings.Builder
	code := run(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestRenderListsEveryCatalogCodeOnceWithItsServerKind(t *testing.T) {
	out := render(apierror.Catalog())

	for _, c := range apierror.Catalog() {
		if got := strings.Count(out, "  '"+c.String()+"',\n"); got != 1 {
			t.Errorf("code %q listed %d times in API_ERROR_CODES", c, got)
		}
		if !strings.Contains(out, ": '"+c.Kind().String()+"',\n") {
			t.Errorf("kind %q of code %q missing", c.Kind(), c)
		}
	}
}

func TestRenderQuotesOnlyKeysThatNeedIt(t *testing.T) {
	out := render(apierror.Catalog())

	if !strings.Contains(out, "\n  internal: 'internal',\n") {
		t.Error("the plain key internal should be unquoted, as prettier writes it")
	}
	if !strings.Contains(out, "\n  'request.invalid_id': 'invalid',\n") {
		t.Error("a dotted key should be quoted")
	}
}

func TestWriteThenCheckPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "apiErrorCodes.generated.ts")

	if code, _, stderr := generate(t, path); code != 0 {
		t.Fatalf("write exited %d: %s", code, stderr)
	}
	if code, _, stderr := generate(t, "--check", path); code != 0 {
		t.Fatalf("check exited %d on a fresh file: %s", code, stderr)
	}
}

func TestCheckFailsOnAStaleFileAndSaysHowToRegenerate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "client", "apiErrorCodes.generated.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("export const API_ERROR_CODES = [] as const;\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, _, stderr := generate(t, "--check", path)

	if code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	if !strings.Contains(stderr, "go run ./scripts/genclientcodes") {
		t.Errorf("stderr does not say how to regenerate: %s", stderr)
	}
}

func TestCheckFailsWhenTheFileIsMissing(t *testing.T) {
	code, _, _ := generate(t, "--check", filepath.Join(t.TempDir(), "absent.ts"))

	if code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
}

func TestUsageWithoutExactlyOnePath(t *testing.T) {
	if code, _, _ := generate(t); code != 2 {
		t.Fatalf("exit code %d, want 2", code)
	}
}
