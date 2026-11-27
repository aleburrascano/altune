package deezer_test

import (
	"altune/go-api/internal/search/internal/adapters/providers/deezer"
	"reflect"
	"testing"
)

func FuzzParseSynchronizedLyrics(f *testing.F) {
	f.Add([]byte(`{"data":{"track":{"lyrics":{"copyright":"c","text":"t","writers":"A, B","synchronizedLines":[{"lrcTimestamp":"[00:01.00]","line":"Hello","milliseconds":1000,"duration":500}]}}}}`))
	f.Add([]byte(`{"data":{"track":{"lyrics":null}}}`))
	f.Add([]byte(`{"errors":[{"message":"boom"}]}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`null`))
	f.Add([]byte(``))
	f.Fuzz(func(t *testing.T, raw []byte) {
		first, firstErr := deezer.ParseSynchronizedLyrics(raw)
		second, secondErr := deezer.ParseSynchronizedLyrics(raw)
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("parsing %q twice gave %+v then %+v", raw, first, second)
		}
		if (firstErr == nil) != (secondErr == nil) {
			t.Fatalf("parsing %q twice failed differently: %v then %v", raw, firstErr, secondErr)
		}
	})
}
