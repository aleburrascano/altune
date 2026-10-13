package catalogtest

import (
	"altune/go-api/internal/catalog/ports"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func AudioStoreContract(t *testing.T, newStore func(t *testing.T) ports.AudioStore) {
	ctx := context.Background()

	source := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "source.opus")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}
		return path
	}

	store := func(t *testing.T, s ports.AudioStore, ref, content string) {
		t.Helper()
		if err := s.Store(ctx, source(t, content), ref); err != nil {
			t.Fatalf("Store %q: %v", ref, err)
		}
	}

	exists := func(t *testing.T, s ports.AudioStore, ref string) bool {
		t.Helper()
		ok, err := s.Exists(ctx, ref)
		if err != nil {
			t.Fatalf("Exists %q: %v", ref, err)
		}
		return ok
	}

	streamed := func(t *testing.T, s ports.AudioStore, ref string) (string, int64) {
		t.Helper()
		rc, size, err := s.Stream(ctx, ref)
		if err != nil {
			t.Fatalf("Stream %q: %v", ref, err)
		}
		defer func() { _ = rc.Close() }()
		got, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("ReadAll %q: %v", ref, err)
		}
		return string(got), size
	}

	t.Run("a ref never stored does not exist and cannot be streamed", func(t *testing.T) {
		s := newStore(t)

		if exists(t, s, "tracks/missing.opus") {
			t.Error("Exists = true for a ref never stored")
		}
		rc, _, err := s.Stream(ctx, "tracks/missing.opus")
		if err == nil {
			_ = rc.Close()
			t.Fatal("Stream of a ref never stored succeeded")
		}
		if !os.IsNotExist(err) {
			t.Errorf("Stream error = %v, want one satisfying os.IsNotExist", err)
		}
	})

	t.Run("a stored ref exists and streams the stored bytes", func(t *testing.T) {
		s := newStore(t)
		content := "fake audio data for the contract"

		store(t, s, "tracks/abc.opus", content)

		if !exists(t, s, "tracks/abc.opus") {
			t.Error("Exists = false after Store")
		}
		got, size := streamed(t, s, "tracks/abc.opus")
		if got != content {
			t.Errorf("streamed content = %q, want %q", got, content)
		}
		if size != int64(len(content)) {
			t.Errorf("streamed size = %d, want %d", size, len(content))
		}
	})

	t.Run("storing the same ref again replaces it", func(t *testing.T) {
		s := newStore(t)

		store(t, s, "tracks/replace.opus", "first version")
		store(t, s, "tracks/replace.opus", "second")

		got, size := streamed(t, s, "tracks/replace.opus")
		if got != "second" {
			t.Errorf("streamed content = %q, want %q", got, "second")
		}
		if size != int64(len("second")) {
			t.Errorf("streamed size = %d, want %d", size, len("second"))
		}
	})

	t.Run("storing leaves other refs untouched", func(t *testing.T) {
		s := newStore(t)

		store(t, s, "tracks/one.opus", "one")
		store(t, s, "tracks/two.opus", "two")

		if got, _ := streamed(t, s, "tracks/one.opus"); got != "one" {
			t.Errorf("one.opus = %q, want %q", got, "one")
		}
	})

	t.Run("a deleted ref no longer exists", func(t *testing.T) {
		s := newStore(t)
		store(t, s, "tracks/gone.opus", "to delete")

		if err := s.Delete(ctx, "tracks/gone.opus"); err != nil {
			t.Fatalf("Delete: %v", err)
		}

		if exists(t, s, "tracks/gone.opus") {
			t.Error("Exists = true after Delete")
		}
		if rc, _, err := s.Stream(ctx, "tracks/gone.opus"); err == nil {
			_ = rc.Close()
			t.Error("Stream succeeded after Delete")
		}
	})

	t.Run("deleting a missing ref is not an error", func(t *testing.T) {
		s := newStore(t)

		if err := s.Delete(ctx, "tracks/never.opus"); err != nil {
			t.Errorf("Delete of a ref never stored = %v, want nil", err)
		}

		store(t, s, "tracks/twice.opus", "to delete twice")
		if err := s.Delete(ctx, "tracks/twice.opus"); err != nil {
			t.Fatalf("first Delete: %v", err)
		}
		if err := s.Delete(ctx, "tracks/twice.opus"); err != nil {
			t.Errorf("second Delete = %v, want nil", err)
		}
	})
}
