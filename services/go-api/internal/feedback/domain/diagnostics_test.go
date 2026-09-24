package domain

import (
	"strings"
	"testing"
)

var (
	rightToLeftOverride = string(rune(0x202E))
	popDirectional      = string(rune(0x202C))
)

func TestDiagnostics_StripsInvisibleAndBidiRunes(t *testing.T) {
	cases := map[string]struct{ dirty, want string }{
		"bidi override":      {"settings" + rightToLeftOverride + "gnp.exe" + popDirectional, "settingsgnp.exe"},
		"zero-width padding": {zeroWidthSpace + "library" + byteOrderMark, "library"},
		"control runes":      {"home\x01\x1b[31m", "home[31m"},
		"invisible-only":     {zeroWidthSpace + wordJoiner, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := (Diagnostics{Screen: tc.dirty}).sanitized().Screen; got != tc.want {
				t.Fatalf("screen = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiagnostics_RedactsSecretsInEveryField(t *testing.T) {
	token := "ghp_" + strings.Repeat("x9", 18)
	clean := NewDiagnostics("1.4.0 "+token, "ios "+token, "18.2 "+token, "settings?token="+token).sanitized()
	want := NewDiagnostics("1.4.0 "+RedactedMarker, "ios "+RedactedMarker, "18.2 "+RedactedMarker, "settings?token="+RedactedMarker)
	if clean != want {
		t.Fatalf("sanitized = %+v, want %+v", clean, want)
	}
}

func TestDiagnostics_RedactsBeforeTruncating(t *testing.T) {
	padding := strings.Repeat("s", maxDiagRunes-10)
	token := "ghp_" + strings.Repeat("x9", 18)
	got := (Diagnostics{Screen: padding + " " + token}).sanitized().Screen
	if strings.Contains(got, "ghp_") || strings.Contains(got, "x9x9") {
		t.Fatalf("screen = %q, a truncated token prefix leaked", got)
	}
}

func TestDiagnostics_RedactsTokenSplitByInvisibleRunes(t *testing.T) {
	token := "ghp_" + zeroWidthSpace + strings.Repeat("x9", 18)
	if got := (Diagnostics{Screen: token}).sanitized().Screen; got != RedactedMarker {
		t.Fatalf("screen = %q, want the rejoined token redacted", got)
	}
}
