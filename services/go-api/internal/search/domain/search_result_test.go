package domain_test

import (
	"altune/go-api/internal/search/domain"
	"testing"
)

func TestSearchResult_WithExtraLeavesTheOriginalUntouched(t *testing.T) {
	for name, extras := range map[string]map[string]any{"no extras": nil, "some extras": {"kept": 1}} {
		t.Run(name, func(t *testing.T) {
			original := domain.SearchResult{Extras: extras}

			changed := original.WithExtra("added", true)

			if _, leaked := original.Extras["added"]; leaked {
				t.Errorf("original extras = %v, want the added key only on the copy", original.Extras)
			}
			if changed.Extras["added"] != true {
				t.Errorf("copy extras = %v, want the added key", changed.Extras)
			}
			if extras != nil && changed.Extras["kept"] != 1 {
				t.Errorf("copy extras = %v, want the existing keys kept", changed.Extras)
			}
		})
	}
}

func TestNewProviderResult(t *testing.T) {
	source := domain.ProviderLink{Provider: domain.ProviderDeezer, ExternalID: "123", URL: "https://deezer.com/track/123"}
	r := domain.NewProviderResult(domain.ResultKindTrack, "HUMBLE.", "Kendrick Lamar", "https://img/x.jpg", source, nil)

	if r.Kind != domain.ResultKindTrack {
		t.Errorf("Kind = %v, want %v", r.Kind, domain.ResultKindTrack)
	}
	if r.Title != "HUMBLE." || r.Subtitle != "Kendrick Lamar" || r.ImageURL != "https://img/x.jpg" {
		t.Errorf("fields not carried: %+v", r)
	}
	if r.Confidence != domain.ConfidenceLow {
		t.Errorf("Confidence = %v, want ConfidenceLow (single-source default)", r.Confidence)
	}
	if len(r.Sources) != 1 || r.Sources[0] != source {
		t.Errorf("Sources = %+v, want exactly [%+v]", r.Sources, source)
	}
	if r.Extras == nil {
		t.Fatal("nil extras must be initialized to a writable map")
	}
	r.Extras["genre"] = "rap"
	if r.Extras["genre"] != "rap" {
		t.Error("Extras must be writable after nil initialization")
	}
}

func TestNewProviderResult_KeepsProvidedExtras(t *testing.T) {
	extras := map[string]any{"preview_url": "https://cdn/p.mp3"}
	r := domain.NewProviderResult(domain.ResultKindAlbum, "DAMN.", "Kendrick Lamar", "", domain.ProviderLink{Provider: domain.ProviderITunes}, extras)

	if r.Extras["preview_url"] != "https://cdn/p.mp3" {
		t.Errorf("provided extras must be kept, got %+v", r.Extras)
	}
}

func TestResultSignature(t *testing.T) {
	tests := []struct {
		name string
		r    domain.SearchResult
		want string
	}{
		{
			name: "track with subtitle",
			r:    domain.SearchResult{Kind: domain.ResultKindTrack, Title: "HUMBLE.", Subtitle: "Kendrick Lamar"},
			want: "track|humble|kendrick lamar",
		},
		{
			name: "artist with empty subtitle keeps trailing separator",
			r:    domain.SearchResult{Kind: domain.ResultKindArtist, Title: "Drake"},
			want: "artist|drake|",
		},
		{
			name: "unicode normalization strips diacritics",
			r:    domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Déjà Vu", Subtitle: "Beyoncé"},
			want: "track|deja vu|beyonce",
		},
		{
			name: "bracketed feat segment dropped, ampersand expanded",
			r:    domain.SearchResult{Kind: domain.ResultKindTrack, Title: "One Dance (feat. Wizkid & Kyla)", Subtitle: "Drake"},
			want: "track|one dance|drake",
		},
		{
			name: "zero-value kind uses the unknown prefix",
			r:    domain.SearchResult{Title: "X"},
			want: "unknown|x|",
		},
		{
			name: "fully empty result",
			r:    domain.SearchResult{},
			want: "unknown||",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ResultSignature(tt.r)
			if got != tt.want {
				t.Errorf("ResultSignature() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResultSignature_KindDisambiguates(t *testing.T) {
	album := domain.SearchResult{Kind: domain.ResultKindAlbum, Title: "DAMN.", Subtitle: "Kendrick Lamar"}
	track := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "DAMN.", Subtitle: "Kendrick Lamar"}
	if domain.ResultSignature(album) == domain.ResultSignature(track) {
		t.Errorf("album and track signatures must differ, both = %q", domain.ResultSignature(album))
	}
}

func TestResultSignature_CaseAndSpacingStable(t *testing.T) {
	a := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Sicko  Mode", Subtitle: "TRAVIS SCOTT"}
	b := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "SICKO MODE", Subtitle: "Travis Scott"}
	if domain.ResultSignature(a) != domain.ResultSignature(b) {
		t.Errorf("signatures must match: %q vs %q", domain.ResultSignature(a), domain.ResultSignature(b))
	}
}

func TestSignatureOf(t *testing.T) {
	derived := domain.SearchResult{Kind: domain.ResultKindTrack, Title: "Hello", Subtitle: "Adele"}
	stamped := derived
	stamped.Signature = "stamped-wins"
	if got := domain.SignatureOf(stamped); got != "stamped-wins" {
		t.Errorf("SignatureOf(stamped) = %q, want the stamped value", got)
	}
	if got, want := domain.SignatureOf(derived), domain.ResultSignature(derived); got != want {
		t.Errorf("SignatureOf(unstamped) = %q, want derived %q", got, want)
	}
}

func TestNewProviderResult_BlanksUnsafeImageURL(t *testing.T) {
	source := domain.ProviderLink{Provider: domain.ProviderDeezer, ExternalID: "1"}
	cases := map[string]string{
		"http":       "http://img.example/a.jpg",
		"javascript": "javascript:alert(1)",
		"data":       "data:image/png;base64,AAAA",
		"no host":    "https:///a.jpg",
		"unparsable": "https://a b\x7f/",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			r := domain.NewProviderResult(domain.ResultKindTrack, "T", "S", raw, source, nil)
			if r.ImageURL != "" {
				t.Errorf("ImageURL = %q, want empty", r.ImageURL)
			}
		})
	}
}

func TestSafeImageURL(t *testing.T) {
	allowed := []string{
		"https://img.example/a.jpg",
		"https://img.example/a.jpg?w=300#x",
		"HTTPS://img.example/a.jpg",
		"https://img.example:8443/a.jpg",
	}
	for _, raw := range allowed {
		t.Run("allowed "+raw, func(t *testing.T) {
			if got := domain.SafeImageURL(raw); got != raw {
				t.Errorf("SafeImageURL(%q) = %q, want it unchanged", raw, got)
			}
		})
	}

	refused := map[string]string{
		"empty":           "",
		"scheme only":     "https://",
		"scheme-relative": "//img.example/a.jpg",
		"opaque":          "https:img.example/a.jpg",
		"ftp":             "ftp://img.example/a.jpg",
		"httpss":          "httpss://img.example/a.jpg",
		"leading space":   " https://img.example/a.jpg",
		"http":            "http://img.example/a.jpg",
	}
	for name, raw := range refused {
		t.Run("refused "+name, func(t *testing.T) {
			if got := domain.SafeImageURL(raw); got != "" {
				t.Errorf("SafeImageURL(%q) = %q, want empty", raw, got)
			}
		})
	}
}

func TestNewProviderResult_KeepsHTTPSImageURLAndDoesNotLeakAcrossCalls(t *testing.T) {
	source := domain.ProviderLink{Provider: domain.ProviderDeezer, ExternalID: "1"}

	valid := domain.NewProviderResult(domain.ResultKindTrack, "T", "S", "https://img.example/a.jpg", source, nil)
	if valid.ImageURL != "https://img.example/a.jpg" {
		t.Errorf("ImageURL = %q, want the https URL unchanged", valid.ImageURL)
	}

	unsafe := domain.NewProviderResult(domain.ResultKindTrack, "T", "S", "http://img.example/a.jpg", source, nil)
	if unsafe.ImageURL != "" {
		t.Errorf("ImageURL = %q, want empty after an earlier valid call", unsafe.ImageURL)
	}
}

func TestPutTypedExtras_CarriesAKnownNotExplicit(t *testing.T) {
	tests := []struct {
		name     string
		explicit *bool
		want     any
		present  bool
	}{
		{"known not explicit", domain.BoolPtr(false), false, true},
		{"known explicit", domain.BoolPtr(true), true, true},
		{"unknown", nil, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extras := map[string]any{}
			domain.PutTypedExtras(extras, domain.SearchResult{Explicit: tt.explicit})

			got, present := extras["explicit"]
			if present != tt.present || got != tt.want {
				t.Errorf("extras[explicit] = %v (present %v), want %v (present %v)", got, present, tt.want, tt.present)
			}
		})
	}
}
