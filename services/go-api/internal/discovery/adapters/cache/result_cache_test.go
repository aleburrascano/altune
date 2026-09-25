package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRedisResultCache_RoundTripAndFreshCopies(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisResultCache(client)
	ctx := context.Background()

	key := fmt.Sprintf("qa-results|%s", t.Name())
	cleanKeys(t, client, hashKey(cache.base.posPrefix, key))

	original := []domain.SearchResult{
		{
			Kind:     domain.ResultKindTrack,
			Title:    "Cached Track",
			Subtitle: "Cached Artist",
			ImageURL: "https://img/1.jpg",
			MBID:     "mbid-1",
			Xref:     map[string]string{"deezer": "42"},
			Sources: []domain.SourceRef{
				{Provider: domain.ProviderDeezer, ExternalID: "42", URL: "https://deezer/42"},
			},
			Album:    "Cached Album",
			Duration: 200,
		},
		{
			Kind:  domain.ResultKindArtist,
			Title: "Cached Artist",
		},
	}
	cache.Set(ctx, key, original)

	got, hit := cache.Get(ctx, key)
	if !hit {
		t.Fatal("expected hit after Set")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	first := got[0]
	if first.Title != "Cached Track" || first.Subtitle != "Cached Artist" ||
		first.Kind != domain.ResultKindTrack || first.MBID != "mbid-1" ||
		first.Album != "Cached Album" || first.Duration != 200 {
		t.Errorf("first result did not round-trip: %+v", first)
	}
	if first.Xref["deezer"] != "42" {
		t.Errorf("Xref did not round-trip: %v", first.Xref)
	}
	if len(first.Sources) != 1 || first.Sources[0].ExternalID != "42" {
		t.Errorf("Sources did not round-trip: %v", first.Sources)
	}

	got[0].Title = "MUTATED"
	got[0].Xref["deezer"] = "corrupted"

	again, hit := cache.Get(ctx, key)
	if !hit {
		t.Fatal("expected hit on second Get")
	}
	if again[0].Title != "Cached Track" {
		t.Errorf("mutation of a returned result leaked into the cache: title = %q", again[0].Title)
	}
	if again[0].Xref["deezer"] != "42" {
		t.Errorf("mutation of a returned nested map leaked into the cache: xref = %v", again[0].Xref)
	}
}

func TestRedisResultCache_KeyIsolationAndMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisResultCache(client)
	ctx := context.Background()

	keyA := fmt.Sprintf("qa-results-a|%s", t.Name())
	keyB := fmt.Sprintf("qa-results-b|%s", t.Name())
	cleanKeys(t, client, hashKey(cache.base.posPrefix, keyA), hashKey(cache.base.posPrefix, keyB))

	cache.Set(ctx, keyA, []domain.SearchResult{{Title: "A"}})

	if _, hit := cache.Get(ctx, keyB); hit {
		t.Error("a different composite key must miss, got hit")
	}
	if _, hit := cache.Get(ctx, "qa-results-never-set|"+t.Name()); hit {
		t.Error("never-set key must miss, got hit")
	}
}

func TestRedisResultCache_CorruptValueIsMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisResultCache(client)
	ctx := context.Background()

	key := fmt.Sprintf("qa-results-corrupt|%s", t.Name())
	redisKey := hashKey(cache.base.posPrefix, key)
	cleanKeys(t, client, redisKey)

	if err := client.Set(ctx, redisKey, "{not json[", 0).Err(); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}
	if got, hit := cache.Get(ctx, key); hit || got != nil {
		t.Errorf("corrupt value Get = (%v,%v), want clean miss", got, hit)
	}
}

// legacyV1ResultPayload is a []domain.SearchResult encoded by the pre-#1084
// code, when record_type and resolution_tier lived in Extras.
const legacyV1ResultPayload = `[{"Kind":2,"Title":"Blue","Subtitle":"Band","ImageURL":"","ArtworkSource":"","Confidence":2,"Sources":[{"Provider":1,"ExternalID":"1","URL":""},{"Provider":10,"ExternalID":"2","URL":""}],"Popularity":0,"ISRC":"","MBID":"","UPC":"123","Xref":null,"Year":0,"ReleaseDate":"","TrackCount":5,"ProviderRank":0,"FanCount":0,"Album":"","Duration":0,"DeezerAlbumID":"","Signature":"","Extras":{"genre_id":1,"record_type":"ep","resolution_tier":"upc","upc":"123"}}]`

// A v1 entry decodes without the typed fields, so serving it would silently
// re-bucket an EP as an album. The key version bump is what keeps new code from
// ever reading one (and old instances from reading v2 entries mid-deploy).
func TestResultCache_legacyV1PayloadIsNotReachable(t *testing.T) {
	var legacy []domain.SearchResult
	if err := json.Unmarshal([]byte(legacyV1ResultPayload), &legacy); err != nil {
		t.Fatalf("decode legacy payload: %v", err)
	}
	if legacy[0].RecordType != "" || legacy[0].ResolutionTier.Stamped {
		t.Fatalf("legacy payload unexpectedly carries typed fields: %+v", legacy[0])
	}
	cache := NewRedisResultCache(nil)
	if cache.base.posPrefix == "discovery:results:v1:" {
		t.Error("result cache still reads v1 keys, whose entries lack RecordType/ResolutionTier")
	}
}

func TestResultCache_typedFieldsRoundTripThroughJSON(t *testing.T) {
	in := []domain.SearchResult{{
		Kind: domain.ResultKindAlbum, Title: "Blue", RecordType: "ep",
		ResolutionTier: domain.StampResolutionTier(domain.EntityResolutionNone),
	}}
	blob, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out []domain.SearchResult
	if err := json.Unmarshal(blob, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out[0].RecordType != "ep" || out[0].ResolutionTier != in[0].ResolutionTier {
		t.Errorf("typed fields did not round-trip: got %+v", out[0])
	}
}

// RecordType is a named string kind, so a v2 entry cached by an instance that
// predates the move still decodes here. Giving it an int kind or a custom
// marshaller would strand every live entry without bumping the key version.
func TestResultCache_recordTypeEncodesAsItsBareString(t *testing.T) {
	blob, err := json.Marshal([]domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "Blue", RecordType: "ep"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), `"RecordType":"ep"`) {
		t.Errorf("cached record type is no longer the bare string: %s", blob)
	}
}
