package cache

import (
	"context"
	"errors"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func identityNormalize(term string) string { return term }

func TestCacheSignal_VocabularyTrigramReadFailureIsLoggedAndCounted(t *testing.T) {
	probe := newSignalProbe(t)
	hook := scriptedRedis{
		single:   func(goredis.Cmder) {},
		pipeline: func([]goredis.Cmder) error { return errors.New("pipeline broke") },
	}
	store := NewVocabularyStore(scriptedClient(t, hook), identityNormalize, WithVocabSignal(probe.signal()))

	if _, err := store.FindClosest(context.Background(), "abcd", 5); err != nil {
		t.Fatalf("FindClosest err = %v, want degraded nil", err)
	}
	if !strings.Contains(probe.logs.String(), "cache=discovery:vocab") {
		t.Errorf("trigram failure not logged: %q", probe.logs.String())
	}
	if got := probe.counter("discovery:vocab.error"); got != "1" {
		t.Errorf("error counter = %s, want 1", got)
	}
}

func TestCacheSignal_VocabularyMetaphoneReadFailureIsLoggedAndCounted(t *testing.T) {
	probe := newSignalProbe(t)
	hook := scriptedRedis{single: func(cmd goredis.Cmder) { cmd.SetErr(errors.New("smembers broke")) }}
	store := NewVocabularyStore(
		scriptedClient(t, hook),
		identityNormalize,
		WithMetaphone(func(string) string { return "ABCT" }),
		WithVocabSignal(probe.signal()),
	)

	if _, err := store.FindClosest(context.Background(), "abcd", 5); err != nil {
		t.Fatalf("FindClosest err = %v, want degraded nil", err)
	}
	if got := probe.counter("discovery:vocab.error"); got != "1" {
		t.Errorf("error counter = %s, want 1", got)
	}
}
