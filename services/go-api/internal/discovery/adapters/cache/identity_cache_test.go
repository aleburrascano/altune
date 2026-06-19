package cache

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// --- nil client safety -------------------------------------------------------

func TestIdentityCache_NilClient_GetReturnsMiss(t *testing.T) {
	cache := NewIdentityCache(nil)
	entry, found := cache.Get(context.Background(), "Artist", "Album")
	if found {
		t.Fatal("expected miss on nil client")
	}
	if entry != nil {
		t.Fatalf("expected nil entry, got %+v", entry)
	}
}

func TestIdentityCache_NilClient_SetNoPanic(t *testing.T) {
	cache := NewIdentityCache(nil)
	cache.Set(context.Background(), "Artist", "Album", identityCacheEntry{
		Verdict:   "confirmed",
		Reason:    "test",
		Layer:     "mb",
		FirstSeen: time.Now(),
	})
	// no panic = pass
}

// --- key generation ----------------------------------------------------------

func TestIdentityCacheKey_Deterministic(t *testing.T) {
	k1 := identityCacheKey("Drake", "Scorpion")
	k2 := identityCacheKey("Drake", "Scorpion")
	if k1 != k2 {
		t.Errorf("same input produced different keys: %q vs %q", k1, k2)
	}
}

func TestIdentityCacheKey_DiffersForDifferentInputs(t *testing.T) {
	k1 := identityCacheKey("Drake", "Scorpion")
	k2 := identityCacheKey("Drake", "Views")
	if k1 == k2 {
		t.Error("different inputs produced the same key")
	}
}

func TestIdentityCacheKey_HasExpectedPrefix(t *testing.T) {
	key := identityCacheKey("Drake", "Scorpion")
	const prefix = "discovery:identity:v1:"
	if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
		t.Errorf("key %q missing expected prefix %q", key, prefix)
	}
}

// --- JSON roundtrip ----------------------------------------------------------

func TestIdentityCacheEntry_JSONRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second).UTC()
	original := identityCacheEntry{
		Verdict:   "contamination",
		Reason:    "genre mismatch",
		Layer:     "genre",
		FirstSeen: now,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded identityCacheEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Verdict != original.Verdict {
		t.Errorf("verdict: got %q, want %q", decoded.Verdict, original.Verdict)
	}
	if decoded.Reason != original.Reason {
		t.Errorf("reason: got %q, want %q", decoded.Reason, original.Reason)
	}
	if decoded.Layer != original.Layer {
		t.Errorf("layer: got %q, want %q", decoded.Layer, original.Layer)
	}
	if !decoded.FirstSeen.Equal(original.FirstSeen) {
		t.Errorf("first_seen: got %v, want %v", decoded.FirstSeen, original.FirstSeen)
	}
}

// --- TTL selection -----------------------------------------------------------

func TestIdentityTTL_ConfirmedIs30Days(t *testing.T) {
	if got := identityTTL("confirmed"); got != identityPositiveTTL {
		t.Errorf("confirmed TTL: got %v, want %v", got, identityPositiveTTL)
	}
}

func TestIdentityTTL_ContaminationIs30Days(t *testing.T) {
	if got := identityTTL("contamination"); got != identityPositiveTTL {
		t.Errorf("contamination TTL: got %v, want %v", got, identityPositiveTTL)
	}
}

func TestIdentityTTL_SuspectIs24Hours(t *testing.T) {
	if got := identityTTL("suspect"); got != identityUnknownTTL {
		t.Errorf("suspect TTL: got %v, want %v", got, identityUnknownTTL)
	}
}

func TestIdentityTTL_UnknownIs24Hours(t *testing.T) {
	if got := identityTTL("unknown"); got != identityUnknownTTL {
		t.Errorf("unknown TTL: got %v, want %v", got, identityUnknownTTL)
	}
}
