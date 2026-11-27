package fansheets_test

import (
	"altune/go-api/internal/search/internal/adapters/providers/fansheets"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func FuzzParseSheet(f *testing.F) {
	paths, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(paths) == 0 {
		f.Fatalf("no fixtures: %v", err)
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			f.Fatalf("read fixture: %v", err)
		}
		f.Add(raw)
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, raw []byte) {
		first, firstErr := fansheets.ParseSheet(raw)
		second, secondErr := fansheets.ParseSheet(raw)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("parsing %q twice gave %+v then %+v", raw, first, second)
		}
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("parsing %q twice failed differently: %v then %v", raw, firstErr, secondErr)
		}
	})
}
