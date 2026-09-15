package cache

import (
	"altune/go-api/internal/discovery/domain"
	"encoding/json"
	"testing"
)

// Redis keys and payloads written before domain.ProviderKey existed must still
// be found and decoded: the golden values below were captured from the
// string-typed implementation.
func TestIdentityKey_ByteIdenticalAcrossProviderKey(t *testing.T) {
	tests := []struct {
		kind       domain.ResultKind
		provider   domain.ProviderKey
		externalID string
		want       string
	}{
		{domain.ResultKindArtist, domain.ProviderKeyDeezer, "27", "discovery:identity:v1:artist:7fb1f072d9817471443055ebf5d7acd9"},
		{domain.ResultKindAlbum, domain.ProviderKeyITunes, "1440818839", "discovery:identity:v1:album:5c60b11e04600784205a0994464645d1"},
	}
	for _, tt := range tests {
		if got := identityKey(tt.kind, tt.provider, tt.externalID); got != tt.want {
			t.Errorf("identityKey(%v, %q, %q) = %q, want %q", tt.kind, tt.provider, tt.externalID, got, tt.want)
		}
	}
}

func TestArtworkEntry_SourceSerializedAsBareString(t *testing.T) {
	entry := artworkEntry{URL: "https://x/a.jpg", Source: domain.ProviderKeyDiscogs.String(), Confidence: 2}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"u":"https://x/a.jpg","s":"discogs","c":2}`; got != want {
		t.Errorf("artworkEntry JSON = %s, want %s", got, want)
	}
	if got, want := artworkCacheKey(domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1"), "discovery:artwork:v3:track:93e2a80d49992538aa72cde4bca801e2"; got != want {
		t.Errorf("artworkCacheKey = %q, want %q", got, want)
	}
}
