package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestNewReport_CountsCombiningMarksAsOneCharacter(t *testing.T) {
	cases := map[string]string{
		"acute accents":       "a" + strings.Repeat(string(rune(0x0301)), MinMessageRunes-1),
		"enclosing circles":   "ok" + strings.Repeat(string(rune(0x20DD)), MinMessageRunes),
		"stacked diacritics":  strings.Repeat("e"+string(rune(0x0300))+string(rune(0x0316)), MinMessageRunes-1),
		"marks on a joiner":   "hi" + zeroWidthJoiner + strings.Repeat(string(rune(0x0301)), MinMessageRunes),
		"marks after a space": "abc " + strings.Repeat(string(rune(0x0301)), MinMessageRunes),
	}
	for name, message := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewReport(reporter(), KindBug, message, Diagnostics{})
			var validation *ValidationError
			if !errors.As(err, &validation) || !strings.Contains(err.Error(), "at least") {
				t.Fatalf("err = %v, want the too-short validation error", err)
			}
		})
	}
}

func TestNewReport_AcceptsTenCharactersWithMarks(t *testing.T) {
	for _, message := range []string{
		strings.Repeat("e"+string(rune(0x0301)), MinMessageRunes),
		"caf" + "e" + string(rune(0x0301)) + " crashes",
		strings.Repeat(string(rune(0x0928))+string(rune(0x093F)), MinMessageRunes),
	} {
		if _, err := NewReport(reporter(), KindBug, message, Diagnostics{}); err != nil {
			t.Fatalf("NewReport(%q): %v, want ten visible characters accepted", message, err)
		}
	}
}
