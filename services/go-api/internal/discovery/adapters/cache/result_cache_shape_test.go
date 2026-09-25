package cache

import (
	"altune/go-api/internal/discovery/domain"
	"encoding/json"
	"strings"
	"testing"
)

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
