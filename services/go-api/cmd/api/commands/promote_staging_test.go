package commands

import (
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
