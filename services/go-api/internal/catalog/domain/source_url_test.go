package domain

import (
	"altune/go-api/internal/shared"
	"errors"
	"strings"
	"testing"
)

func TestValidateSourceURL_RejectsNonPublicHosts(t *testing.T) {
	internal := []string{
		"http://169.254.169.254/latest/meta-data/iam/security-credentials/",
		"http://127.0.0.1/admin",
		"http://127.0.0.1:8080/",
		"https://127.1.2.3/",
		"http://0.0.0.0/",
		"http://0.1.2.3/",
		"http://10.0.0.5/",
		"http://172.16.0.1/",
		"http://192.168.1.1/",
		"http://100.64.0.1/",
		"http://198.18.0.1/",
		"http://192.0.0.170/",
		"http://224.0.0.1/",
		"http://255.255.255.255/",
		"http://[::1]/",
		"http://[::]/",
		"http://[::ffff:127.0.0.1]/",
		"http://[::ffff:169.254.169.254]/",
		"http://[::7f00:1]/",
		"http://[64:ff9b::7f00:1]/",
		"http://[2002:7f00:1::]/",
		"http://[fe80::1%25eth0]/",
		"http://[fc00::1]/",
		"http://[fd00:ec2::254]/",
		"http://localhost/",
		"http://LOCALHOST:3000/",
		"http://localhost./",
		"http://api.localhost/",
		"http://metadata.google.internal/computeMetadata/v1/",
		"http://metadata/",
		"http://2130706433/",
		"http://0x7f000001/",
		"http://0x7f.1/",
		"http://127.1/",
		"http://0177.0.0.1/",
		"http://user:pass@127.0.0.1/",
		"http://example.com@169.254.169.254/",
		"  http://127.0.0.1/  ",
		"http://１２７.０.０.１/",
		"http://ｌｏｃａｌｈｏｓｔ/",
		"http://127。0。0。1/",
		"http://0X7F000001/",
		"http://[::ffff:7f00:1]/",
	}
	for _, raw := range internal {
		t.Run(raw, func(t *testing.T) {
			err := ValidateSourceURL(raw)
			var verr *shared.ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("ValidateSourceURL(%q) = %v, want a validation error", raw, err)
			}
			if !strings.Contains(err.Error(), "public host") {
				t.Fatalf("error = %q, want it to mention a public host", err.Error())
			}
		})
	}
}

// Host smuggling that other URL parsers would read as an internal address must
// not slip past as a public host; Go refuses these as malformed, which is also a
// rejection.
func TestValidateSourceURL_RejectsSmuggledHosts(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1\\@example.com/",
		"http://%31%32%37.0.0.1/",
		"http://127.0.0.1%09.example.com/",
	} {
		t.Run(raw, func(t *testing.T) {
			var verr *shared.ValidationError
			if err := ValidateSourceURL(raw); !errors.As(err, &verr) {
				t.Fatalf("ValidateSourceURL(%q) = %v, want a validation error", raw, err)
			}
		})
	}
}

func TestValidateSourceURL_AcceptsPublicHosts(t *testing.T) {
	public := []string{
		"",
		"https://soundcloud.com/artist/track",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://artist.bandcamp.com/track/song",
		"https://example.com/song.mp3",
		"http://8.8.8.8/song.mp3",
		"https://[2606:4700:4700::1111]/song.mp3",
		"https://cdn.example.com:8443/a.mp3",
		"https://0xdeadbeef.example.com/a.mp3",
		"https://123.example.com/a.mp3",
		"https://localhost.example.com/a.mp3",
	}
	for _, raw := range public {
		t.Run(raw, func(t *testing.T) {
			if err := ValidateSourceURL(raw); err != nil {
				t.Fatalf("ValidateSourceURL(%q) = %v, want nil", raw, err)
			}
		})
	}
}
