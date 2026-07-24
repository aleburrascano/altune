package phonetics

import "testing"

func TestDoubleMetaphone(t *testing.T) {
	tests := []struct {
		input   string
		primary string
	}{
		{"weeknd", "AKNT"},
		{"weekend", "AKNT"},
		{"smith", "SM0"},
		{"schmidt", "SKMT"},
		{"phone", "FN"},
		{"knight", "NKT"},
		{"wright", "RKT"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_PhoneticEquivalence(t *testing.T) {
	pairs := []struct {
		a, b string
	}{
		{"weeknd", "weekend"},
		{"justin", "justen"},
		{"ariana", "arianna"},
		{"mac", "mack"},
	}
	for _, tt := range pairs {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			pA, _ := DoubleMetaphone(tt.a)
			pB, _ := DoubleMetaphone(tt.b)
			if pA != pB {
				t.Errorf("DoubleMetaphone(%q)=%q != DoubleMetaphone(%q)=%q — expected same code",
					tt.a, pA, tt.b, pB)
			}
		})
	}
}

func TestDoubleMetaphone_SilentInitials(t *testing.T) {
	tests := []struct {
		input   string
		primary string
	}{
		{"knight", "NKT"}, // KN → N
		{"gnome", "NM"},   // GN → N
		{"wright", "RKT"}, // WR → R
		{"aeon", "N"},     // AE → E (then leading-vowel rule consumed)
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_Digraphs(t *testing.T) {
	tests := []struct {
		input   string
		primary string
	}{
		{"cher", "XR"},     // CH → X
		{"cello", "SL"},    // CE → S, LL collapsed
		{"jack", "JK"},     // CK collapsed to one K
		{"edge", "AJ"},     // DGE → J
		{"phone", "FN"},    // PH → F
		{"the", "0"},       // TH → 0
		{"nation", "NXN"},  // TIO → X
		{"school", "SKL"},  // SCH → SK
		{"science", "SNS"}, // SCI → S
		{"scat", "SKT"},    // SC+other → SK
		{"ghost", "KST"},   // initial GH → K
		{"ghetto", "KT"},   // GH after consonantless start → K, TT collapsed
		{"night", "NKT"},   // GH after vowel → K (simplified; real metaphone silences it)
		{"xavier", "KSFR"}, // X → KS
		{"exxon", "AKSN"},  // XX collapsed
		{"queen", "KN"},    // QU → K
		{"pizza", "PS"},    // ZZ → S
		{"jazz", "JS"},     // trailing ZZ → S
		{"viva", "FF"},     // V → F
		{"judge", "JJ"},    // DGE → J after initial J
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_DoubledLetters(t *testing.T) {
	tests := []struct {
		input   string
		primary string
	}{
		{"dodd", "TT"},     // final DD collapses to one T (initial D emits the other)
		{"off", "AF"},      // FF → F, leading vowel kept
		{"happy", "HP"},    // PP → P
		{"bubba", "PP"},    // BB → P
		{"accent", "AKNT"}, // CC → K then continues past both
		{"buggy", "PK"},    // GG → K
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_RemainingBranches(t *testing.T) {
	tests := []struct {
		input   string
		primary string
	}{
		{"afghan", "AFN"},  // mid-word GH after consonant is silent
		{"signs", "SS"},    // mid-word GN followed by consonant is silent
		{"shakira", "XKR"}, // SH → X
		{"hajj", "HJ"},     // JJ collapsed
		{"mokka", "MK"},    // KK collapsed
		{"zaqqum", "SKM"},  // QQ collapsed
		{"ferrari", "FRR"}, // RR collapsed
		{"missy", "MS"},    // SS collapsed
		{"savvy", "SF"},    // VV collapsed
		{"zebra", "SPR"},   // single Z → S
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_VowelHandling(t *testing.T) {
	// Only a leading vowel is encoded (as A); interior vowels are dropped.
	tests := []struct {
		input   string
		primary string
	}{
		{"ooh", "A"},  // leading vowel, trailing H silent
		{"wa", "A"},   // W+vowel → A
		{"a", "A"},    // single vowel
		{"hah", "H"},  // H before vowel at word start kept, trailing H dropped
		{"high", "HK"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_NonAlphaAndUnicode(t *testing.T) {
	tests := []struct {
		input   string
		primary string
	}{
		{"ab3d", "APT"},    // digit skipped, consonants both sides survive
		{"123", ""},        // all digits → empty code
		{"!?", ""},         // symbols only → empty code
		{"élan", "LN"},     // non-ASCII letter skipped (not treated as a vowel)
		{"x", "KS"},        // single consonant
		{"q", "K"},
		{"  smith  ", "SM0"}, // surrounding whitespace trimmed
		{"SMITH", "SM0"},     // case-insensitive
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_SoundAlikePairs(t *testing.T) {
	pairs := []struct {
		a, b string
	}{
		{"kanye", "kanyay"},
		{"their", "there"},
		{"smyth", "smith"},
		{"muhammad", "mohammed"},
		{"stefani", "stephani"},
		{"clark", "clarke"},
	}
	for _, tt := range pairs {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			pA, _ := DoubleMetaphone(tt.a)
			pB, _ := DoubleMetaphone(tt.b)
			if pA != pB {
				t.Errorf("DoubleMetaphone(%q)=%q != DoubleMetaphone(%q)=%q — expected same code",
					tt.a, pA, tt.b, pB)
			}
		})
	}
}

func TestDoubleMetaphone_MaxLengthFour(t *testing.T) {
	words := []string{"kendrick", "muhammad", "administrator", "supercalifragilistic"}
	for _, w := range words {
		t.Run(w, func(t *testing.T) {
			pri, alt := DoubleMetaphone(w)
			if len(pri) > 4 || len(alt) > 4 {
				t.Errorf("DoubleMetaphone(%q) = (%q, %q) — codes must be capped at 4", w, pri, alt)
			}
		})
	}
}

func TestDoubleMetaphone_Stability(t *testing.T) {
	// Same input → same key, and casing must not change the key: the vocabulary
	// index writes the key at Add time and matches it at query time.
	words := []string{"weeknd", "Beyonce", "kendrick lamar", "SMITH"}
	for _, w := range words {
		t.Run(w, func(t *testing.T) {
			p1, a1 := DoubleMetaphone(w)
			p2, a2 := DoubleMetaphone(w)
			if p1 != p2 || a1 != a2 {
				t.Errorf("DoubleMetaphone(%q) unstable: (%q,%q) then (%q,%q)", w, p1, a1, p2, a2)
			}
		})
	}
}

// TestDoubleMetaphone_SimplifiedQuirks pins behavior where this simplified
// implementation diverges from canonical (double) metaphone. Matching is
// symmetric — both sides of a comparison use the same function — so these are
// consistency anchors, not correctness claims: if one changes, every stored
// vocabulary metaphone key built from it goes stale.
func TestDoubleMetaphone_SimplifiedQuirks(t *testing.T) {
	tests := []struct {
		input   string
		primary string
		note    string
	}{
		{"who", "", "W before consonant dropped, H before vowel after consonant dropped — whole word encodes to nothing"},
		{"glow", "L", "G before consonant (not H/N/G) is dropped, not encoded as K"},
		{"grande", "RNT", "same G-before-consonant drop: collides with 'rande'"},
		{"gig", "K", "trailing G (not followed by vowel) dropped"},
		{"psycho", "PSX", "initial PS is not treated as silent (canonical: SK-)"},
		{"sign", "SKN", "word-final GN encodes as KN instead of N"},
		{"laugh", "LK", "GH after vowel encodes as K (canonical: F/silent)"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want pinned %q (%s)", tt.input, pri, tt.primary, tt.note)
			}
		})
	}
}

func TestDoubleMetaphone_AlternateMirrorsPrimary(t *testing.T) {
	// Documents current behavior: no branch ever emits a divergent alternate,
	// so the second return value always equals the primary.
	words := []string{"weeknd", "schmidt", "cello", "xavier", "night", "jose"}
	for _, w := range words {
		t.Run(w, func(t *testing.T) {
			pri, alt := DoubleMetaphone(w)
			if pri != alt {
				t.Errorf("DoubleMetaphone(%q) = (%q, %q) — alternate expected to mirror primary", w, pri, alt)
			}
		})
	}
}

func TestMetaphoneKey(t *testing.T) {
	tests := []struct {
		term string
		want string
	}{
		{"the weeknd", "0AKNT"},
		{"the weekend", "0AKNT"},
		{"tay k", "TK"},
		{"", ""},
		{"   ", ""},                 // whitespace-only → no words
		{"  the   weeknd  ", "0AKNT"}, // ragged spacing collapses via Fields
		{"...", ""},                 // symbol-only word stripped, then skipped
		{"!!! chk", "XK"},           // symbol-only word skipped, real word coded
		{"a.b 123", "AP"},           // punctuation stripped inside word; digit-only word skipped
		{"AC/DC", "AKTK"},           // slash stripped → single word ACDC
		{"björk", "PJRK"},           // non-ASCII letter survives stripNonAlpha, skipped by the coder
	}
	for _, tt := range tests {
		t.Run(tt.term, func(t *testing.T) {
			got := MetaphoneKey(tt.term)
			if got != tt.want {
				t.Errorf("MetaphoneKey(%q) = %q, want %q", tt.term, got, tt.want)
			}
		})
	}
}
