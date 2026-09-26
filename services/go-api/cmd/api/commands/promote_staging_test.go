package commands

import (
	"altune/go-api/internal/catalog/adapters/storage"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestProdRefFor(t *testing.T) {
	stagingUser := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	prodUser := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	otherUser := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	t.Run("strips the staging prefix and swaps the user segment", func(t *testing.T) {
		ref := "staging/" + stagingUser.String() + "/Artist/Album/Song.opus"
		got, err := prodRefFor(ref, stagingUser, prodUser)
		if err != nil {
			t.Fatalf("prodRefFor: %v", err)
		}
		want := prodUser.String() + "/Artist/Album/Song.opus"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("refuses a ref whose first segment is a foreign uuid", func(t *testing.T) {
		ref := "staging/" + otherUser.String() + "/Artist/Album/Song.opus"
		_, err := prodRefFor(ref, stagingUser, prodUser)
		if err == nil {
			t.Fatal("expected an error refusing the foreign uuid, got nil")
		}
		if !strings.Contains(err.Error(), "does not match owning account") {
			t.Errorf("error should name the mismatch, got: %v", err)
		}
	})

	t.Run("refuses a ref with no staging prefix", func(t *testing.T) {
		_, err := prodRefFor(stagingUser.String()+"/Artist/Album/Song.opus", stagingUser, prodUser)
		if err == nil {
			t.Fatal("expected an error for a ref missing the staging/ prefix, got nil")
		}
	})

	t.Run("refuses a ref with no path after the user segment", func(t *testing.T) {
		_, err := prodRefFor("staging/"+stagingUser.String(), stagingUser, prodUser)
		if err == nil {
			t.Fatal("expected an error for a ref with no user segment, got nil")
		}
	})

	t.Run("refuses a remainder that traverses out to another user's directory", func(t *testing.T) {
		ref := "staging/" + stagingUser.String() + "/../" + otherUser.String() + "/x.opus"
		_, err := prodRefFor(ref, stagingUser, prodUser)
		if err == nil {
			t.Fatal("expected an error refusing the traversal, got nil")
		}
		if !strings.Contains(err.Error(), "unsafe path segment") {
			t.Errorf("error should name the unsafe segment, got: %v", err)
		}
	})

	t.Run("refuses a remainder with an embedded .. segment", func(t *testing.T) {
		ref := "staging/" + stagingUser.String() + "/Artist/../../" + otherUser.String() + "/x.opus"
		_, err := prodRefFor(ref, stagingUser, prodUser)
		if err == nil {
			t.Fatal("expected an error refusing the embedded traversal, got nil")
		}
		if !strings.Contains(err.Error(), "unsafe path segment") {
			t.Errorf("error should name the unsafe segment, got: %v", err)
		}
	})

	t.Run("refuses a remainder with an empty path segment", func(t *testing.T) {
		ref := "staging/" + stagingUser.String() + "//Song.opus"
		_, err := prodRefFor(ref, stagingUser, prodUser)
		if err == nil {
			t.Fatal("expected an error refusing the empty segment, got nil")
		}
		if !strings.Contains(err.Error(), "unsafe path segment") {
			t.Errorf("error should name the unsafe segment, got: %v", err)
		}
	})
}

func TestProdUserIDFor(t *testing.T) {
	prod := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	staging := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	accounts := accountMap{prod: staging}

	if got := prodUserIDFor(accounts, staging); got != prod {
		t.Errorf("got %s, want %s", got, prod)
	}
	if got := prodUserIDFor(accounts, uuid.New()); got != uuid.Nil {
		t.Errorf("unmatched staging id: got %s, want uuid.Nil", got)
	}
}

func TestIndexOf(t *testing.T) {
	cols := []string{"id", "user_id", "audio_ref"}
	if got := indexOf(cols, "user_id"); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
	if got := indexOf(cols, "missing"); got != -1 {
		t.Errorf("got %d, want -1", got)
	}
}

func TestProdRefStillFree(t *testing.T) {
	musicDir := t.TempDir()
	store := storage.NewFilesystemAudioStore(musicDir)
	ctx := context.Background()
	prodRef := "22222222-2222-2222-2222-222222222222/Artist/Album/Song.mp3"

	free, err := prodRefStillFree(ctx, store, prodRef)
	if err != nil {
		t.Fatalf("prodRefStillFree: %v", err)
	}
	if !free {
		t.Fatal("expected prodRef free when nothing has been written there yet")
	}

	prodFile := filepath.Join(musicDir, prodRef)
	if err := os.MkdirAll(filepath.Dir(prodFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(prodFile, []byte("landed after the skip check"), 0o644); err != nil {
		t.Fatal(err)
	}

	free, err = prodRefStillFree(ctx, store, prodRef)
	if err != nil {
		t.Fatalf("prodRefStillFree: %v", err)
	}
	if free {
		t.Fatal("expected prodRef no longer free once an object landed there, so promoteOne refuses to overwrite it")
	}
}
