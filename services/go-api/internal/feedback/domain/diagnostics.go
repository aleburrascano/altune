package domain

import (
	"strings"
	"unicode/utf8"
)

type Diagnostics struct {
	AppVersion string
	Platform   string
	OSVersion  string
	Screen     string
}

func NewDiagnostics(appVersion, platform, osVersion, screen string) Diagnostics {
	return Diagnostics{
		AppVersion: appVersion,
		Platform:   platform,
		OSVersion:  osVersion,
		Screen:     screen,
	}
}

var diagnosticsFields = []struct {
	get func(Diagnostics) string
	set func(*Diagnostics, string)
}{
	{func(d Diagnostics) string { return d.AppVersion }, func(d *Diagnostics, v string) { d.AppVersion = v }},
	{func(d Diagnostics) string { return d.Platform }, func(d *Diagnostics, v string) { d.Platform = v }},
	{func(d Diagnostics) string { return d.OSVersion }, func(d *Diagnostics, v string) { d.OSVersion = v }},
	{func(d Diagnostics) string { return d.Screen }, func(d *Diagnostics, v string) { d.Screen = v }},
}

func (d Diagnostics) sanitized() Diagnostics {
	var out Diagnostics
	for _, f := range diagnosticsFields {
		f.set(&out, singleLine(f.get(d)))
	}
	return out
}

func singleLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, maxDiagRunes)
}

func truncate(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	return strings.TrimSpace(string(runes[:clusterBoundaryAtOrBefore(runes, limit-1)])) + "…"
}
