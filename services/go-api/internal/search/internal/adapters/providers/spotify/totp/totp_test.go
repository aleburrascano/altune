package totp_test

import (
	"altune/go-api/internal/search/internal/adapters/providers/spotify/totp"
	"testing"
)

const rfc6238Key = "12345678901234567890"

func secretDerivingKey(key string) string {
	secret := make([]byte, len(key))
	for i := range len(key) {
		secret[i] = (key[i] - '0') ^ byte(i%33+9)
	}
	return string(secret)
}

func TestCode_matchesTheRFC6238SHA1VectorsForTheSecretThatDerivesTheirKey(t *testing.T) {
	secret := secretDerivingKey(rfc6238Key)
	for unixSeconds, want := range map[int64]string{
		59:          "287082",
		1111111109:  "081804",
		1111111111:  "050471",
		1234567890:  "005924",
		2000000000:  "279037",
		20000000000: "353130",
	} {
		if got := totp.Code(secret, unixSeconds); got != want {
			t.Errorf("Code(rfc secret, %d) = %q, want %q (RFC 6238 SHA1 vector, last 6 digits)", unixSeconds, got, want)
		}
	}
}

func TestCode_derivesTheKeyFromEverySecretByte(t *testing.T) {
	for _, c := range []struct {
		secret      string
		unixSeconds int64
		want        string
	}{
		{"AB", 1700000000, "036312"},
		{`,7/*F("rLJ2oxaKL^f+E1xvP@N`, 1700000000, "371599"},
		{`{iOFn;4}<1PFYKPV?5{%u14]M>/V0hDH`, 1700000000, "419531"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn", 1700000000, "934208"},
	} {
		if got := totp.Code(c.secret, c.unixSeconds); got != c.want {
			t.Errorf("Code(%q, %d) = %q, want %q", c.secret, c.unixSeconds, got, c.want)
		}
	}
}

func TestCode_isStableWithinA30SecondStepAndChangesAcrossSteps(t *testing.T) {
	const stepStart = 1700000010
	first := totp.Code("AB", stepStart)
	if last := totp.Code("AB", stepStart+29); last != first {
		t.Errorf("codes within one step differ: %q at %d, %q at %d", first, stepStart, last, stepStart+29)
	}
	if next := totp.Code("AB", stepStart+30); next == first {
		t.Errorf("codes for adjacent steps collided: both %q", first)
	}
	if previous := totp.Code("AB", stepStart-1); previous == first {
		t.Errorf("code one second before the step start equals the step's code %q", first)
	}
}
