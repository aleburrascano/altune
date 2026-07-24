package domain

import "testing"

func TestResolutionTierFromExtras(t *testing.T) {
	tests := []struct {
		name   string
		extras map[string]any
		want   EntityResolutionTier
	}{
		{name: "mbid", extras: map[string]any{"resolution_tier": "mbid"}, want: EntityResolutionMBID},
		{name: "isrc", extras: map[string]any{"resolution_tier": "isrc"}, want: EntityResolutionISRC},
		{name: "upc", extras: map[string]any{"resolution_tier": "upc"}, want: EntityResolutionUPC},
		{name: "bridge", extras: map[string]any{"resolution_tier": "bridge"}, want: EntityResolutionBridge},
		{name: "unrecognized string", extras: map[string]any{"resolution_tier": "vibes"}, want: EntityResolutionNone},
		{name: "key absent", extras: map[string]any{}, want: EntityResolutionNone},
		{name: "nil extras", extras: nil, want: EntityResolutionNone},
		{name: "wrong type", extras: map[string]any{"resolution_tier": 3}, want: EntityResolutionNone},
		// "none" is a String() output, not a value merge.go ever stamps — it
		// still maps to the None sentinel.
		{name: "none", extras: map[string]any{"resolution_tier": "none"}, want: EntityResolutionNone},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResolutionTierFromExtras(tt.extras)
			if got != tt.want {
				t.Errorf("ResolutionTierFromExtras(%v) = %v, want %v", tt.extras, got, tt.want)
			}
		})
	}
}

func TestResolutionTierFromExtras_RoundTrip(t *testing.T) {
	// Every tier merge.go can stamp must survive String() -> FromExtras.
	tiers := []EntityResolutionTier{
		EntityResolutionISRC, EntityResolutionUPC,
		EntityResolutionMBID, EntityResolutionBridge,
	}
	for _, tier := range tiers {
		t.Run(tier.String(), func(t *testing.T) {
			got := ResolutionTierFromExtras(map[string]any{"resolution_tier": tier.String()})
			if got != tier {
				t.Errorf("round-trip: got %v, want %v", got, tier)
			}
		})
	}
}

func TestNewProviderResult(t *testing.T) {
	source := SourceRef{Provider: ProviderDeezer, ExternalID: "123", URL: "https://deezer.com/track/123"}
	r := NewProviderResult(ResultKindTrack, "HUMBLE.", "Kendrick Lamar", "https://img/x.jpg", source, nil)

	if r.Kind != ResultKindTrack {
		t.Errorf("Kind = %v, want %v", r.Kind, ResultKindTrack)
	}
	if r.Title != "HUMBLE." || r.Subtitle != "Kendrick Lamar" || r.ImageURL != "https://img/x.jpg" {
		t.Errorf("fields not carried: %+v", r)
	}
	if r.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %v, want ConfidenceLow (single-source default)", r.Confidence)
	}
	if len(r.Sources) != 1 || r.Sources[0] != source {
		t.Errorf("Sources = %+v, want exactly [%+v]", r.Sources, source)
	}
	if r.Extras == nil {
		t.Fatal("nil extras must be initialized to a writable map")
	}
	// The nil-map footgun: the returned Extras must be writable.
	r.Extras["genre"] = "rap"
	if r.Extras["genre"] != "rap" {
		t.Error("Extras must be writable after nil initialization")
	}
}

func TestNewProviderResult_KeepsProvidedExtras(t *testing.T) {
	extras := map[string]any{"preview_url": "https://cdn/p.mp3"}
	r := NewProviderResult(ResultKindAlbum, "DAMN.", "Kendrick Lamar", "", SourceRef{Provider: ProviderITunes}, extras)

	if r.Extras["preview_url"] != "https://cdn/p.mp3" {
		t.Errorf("provided extras must be kept, got %+v", r.Extras)
	}
}

func TestResultSignature(t *testing.T) {
	// Byte-exactness matters: the rank pipeline and the wire mapper MUST
	// compute the same string or behavioral scores stop joining.
	tests := []struct {
		name string
		r    SearchResult
		want string
	}{
		{
			name: "track with subtitle",
			r:    SearchResult{Kind: ResultKindTrack, Title: "HUMBLE.", Subtitle: "Kendrick Lamar"},
			want: "track|humble|kendrick lamar",
		},
		{
			name: "artist with empty subtitle keeps trailing separator",
			r:    SearchResult{Kind: ResultKindArtist, Title: "Drake"},
			want: "artist|drake|",
		},
		{
			name: "unicode normalization strips diacritics",
			r:    SearchResult{Kind: ResultKindTrack, Title: "Déjà Vu", Subtitle: "Beyoncé"},
			want: "track|deja vu|beyonce",
		},
		{
			name: "bracketed feat segment dropped, ampersand expanded",
			r:    SearchResult{Kind: ResultKindTrack, Title: "One Dance (feat. Wizkid & Kyla)", Subtitle: "Drake"},
			want: "track|one dance|drake",
		},
		{
			name: "zero-value kind uses the unknown prefix",
			r:    SearchResult{Title: "X"},
			want: "unknown|x|",
		},
		{
			name: "fully empty result",
			r:    SearchResult{},
			want: "unknown||",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ResultSignature(tt.r)
			if got != tt.want {
				t.Errorf("ResultSignature() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResultSignature_KindDisambiguates(t *testing.T) {
	// Same title+subtitle, different kind → different signature: an album and
	// its title track must never share an engagement join key.
	album := SearchResult{Kind: ResultKindAlbum, Title: "DAMN.", Subtitle: "Kendrick Lamar"}
	track := SearchResult{Kind: ResultKindTrack, Title: "DAMN.", Subtitle: "Kendrick Lamar"}
	if ResultSignature(album) == ResultSignature(track) {
		t.Errorf("album and track signatures must differ, both = %q", ResultSignature(album))
	}
}

func TestResultSignature_CaseAndSpacingStable(t *testing.T) {
	// Provider casing/spacing variance must not fracture the join key.
	a := SearchResult{Kind: ResultKindTrack, Title: "Sicko  Mode", Subtitle: "TRAVIS SCOTT"}
	b := SearchResult{Kind: ResultKindTrack, Title: "SICKO MODE", Subtitle: "Travis Scott"}
	if ResultSignature(a) != ResultSignature(b) {
		t.Errorf("signatures must match: %q vs %q", ResultSignature(a), ResultSignature(b))
	}
}
