package ports_test

import (
	"altune/go-api/internal/download/internal/core/ports"
	"encoding/json"
	"reflect"
	"testing"
)

func TestAppendRejectedSourceKeys_IsIdempotentIgnoresEmptyAndBounded(t *testing.T) {
	t.Parallel()

	keys := ports.AppendRejectedSourceKeys(nil, []string{"youtube:aaa", "youtube:aaa", ""})
	if !reflect.DeepEqual(keys, []string{"youtube:aaa"}) {
		t.Fatalf("keys = %v, want one key with the duplicate and the empty key ignored", keys)
	}

	var added []string
	for i := range 60 {
		added = append(added, string(rune('a'+i%26))+string(rune('0'+i/26)))
	}
	keys = ports.AppendRejectedSourceKeys(nil, added)
	if want := added[len(added)-ports.MaxRejectedSourceKeys:]; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %v, want the %d most recent rejections %v", keys, ports.MaxRejectedSourceKeys, want)
	}
}

func TestMergeUpgradeEvidence_StateOwnsTheUpgradeKeysAndTheTrackOwnsTheRest(t *testing.T) {
	t.Parallel()

	track := json.RawMessage(`{"grade":"B","verdict":"match","upgrade_tries":7,"in_place_failures":2}`)
	state := json.RawMessage(`{"upgrade_tries":1}`)

	merged, err := ports.MergeUpgradeEvidence(track, state)
	if err != nil {
		t.Fatalf("MergeUpgradeEvidence = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("merged evidence is not JSON: %v", err)
	}
	want := map[string]any{"grade": "B", "verdict": "match", "upgrade_tries": float64(1)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("merged = %v, want %v (a stale upgrade key on the track must not leak through)", got, want)
	}
}

func TestMergeUpgradeEvidence_NoEvidenceStaysNone(t *testing.T) {
	t.Parallel()

	merged, err := ports.MergeUpgradeEvidence(nil, nil)
	if err != nil || len(merged) != 0 {
		t.Errorf("MergeUpgradeEvidence(nil, nil) = %q, %v, want empty and nil", merged, err)
	}
}

func TestMergeUpgradeEvidence_ANullTrackEvidenceTakesTheStateKeysWithoutPanicking(t *testing.T) {
	t.Parallel()

	merged, err := ports.MergeUpgradeEvidence(json.RawMessage(`null`), json.RawMessage(`{"upgrade_tries":1}`))
	if err != nil || string(merged) != `{"upgrade_tries":1}` {
		t.Errorf("MergeUpgradeEvidence = %s, %v, want the state keys", merged, err)
	}
}
