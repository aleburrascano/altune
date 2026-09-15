package eval

import (
	"encoding/json"
	"testing"
)

// TestOutcomeEnumsMarshalJSON pins the exact JSON bytes of every outcome enum
// value; the eval baselines and the discovery-eval-gate workflow consume them.
func TestOutcomeEnumsMarshalJSON(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"ArtistIntentUnknown", ArtistIntentUnknown, `"unknown"`},
		{"ArtistIntentPass", ArtistIntentPass, `"pass"`},
		{"ArtistIntentBuried", ArtistIntentBuried, `"buried"`},
		{"ArtistIntentBelowK", ArtistIntentBelowK, `"below_k"`},
		{"ArtistIntentAbsent", ArtistIntentAbsent, `"absent"`},
		{"ArtistIntentNoResults", ArtistIntentNoResults, `"no_results"`},
		{"ArtistIntentSkipped", ArtistIntentSkipped, `"skipped"`},
		{"ArtistIntentOutOfRange", ArtistIntentOutcome(99), `"unknown"`},
		{"GapStrengthUnknown", GapStrengthUnknown, `"unknown"`},
		{"GapStrong", GapStrong, `"strong"`},
		{"GapWeak", GapWeak, `"weak"`},
		{"GapAbandoned", GapAbandoned, `"abandoned"`},
		{"GapStrengthOutOfRange", GapStrength(99), `"unknown"`},
		{"EvalOutcomeUnknown", EvalOutcomeUnknown, `"unknown"`},
		{"EvalPass", EvalPass, `"pass"`},
		{"EvalFailWrongTop", EvalFailWrongTop, `"fail_wrong_top"`},
		{"EvalFailNoResults", EvalFailNoResults, `"fail_no_results"`},
		{"EvalSkipped", EvalSkipped, `"skipped"`},
		{"EvalOutcomeOutOfRange", EvalOutcome(99), `"unknown"`},
		{"EmbeddedInStruct", struct {
			A ArtistIntentOutcome `json:"a"`
			G GapStrength         `json:"g"`
			E EvalOutcome         `json:"e"`
		}{ArtistIntentBelowK, GapWeak, EvalFailWrongTop}, `{"a":"below_k","g":"weak","e":"fail_wrong_top"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
