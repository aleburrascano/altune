package providers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"strconv"
	"strings"
)

// --- TOTP token derivation ---------------------------------------------
//
// Spotify's web player gates its anonymous access-token endpoint
// (open.spotify.com/api/token) behind a TOTP code computed from a secret
// embedded in the player's JS bundle — the same mechanism librespot/zotify
// fight, confirmed live 2026-07-22 (a plain request without a valid `totp`
// param returns 403). Secrets and the derivation below are ported from the
// existing open-source reverse-engineering in AliAkhtari78/SpotifyScraper
// (MIT-licensed, src/spotify_scraper/auth/totp.py) rather than re-discovered
// independently.
//
// AIDEV-WARNING: Spotify rotates these secrets periodically (that project's
// own issue history documents at least one prior break-and-rewrite cycle).
// When every version below returns "totpVerExpired", the fix is pulling the
// current secret table from that project (or wherever it has since moved) —
// there is no way to derive a new secret analytically, it must be re-scraped
// from Spotify's obfuscated JS by whoever's tooling stays current on it.

// spotifyTOTPSecret is one versioned secret; the resolver tries versions
// newest-first and falls back on a "totpVerExpired" response.
type spotifyTOTPSecret struct {
	version int
	secret  string
}

var spotifyTOTPSecrets = []spotifyTOTPSecret{
	{61, `,7/*F("rLJ2oxaKL^f+E1xvP@N`},
	{60, `OmE{ZA.J^":0FG\\Uz?[@WW`},
	{59, `{iOFn;4}<1PFYKPV?5{%u14]M>/V0hDH`},
}

const (
	spotifyTOTPPeriod = 30
	spotifyTOTPDigits = 6
)

// spotifyTOTPKey derives the HMAC key from a secret: XOR each character's
// code point with (index % 33 + 9), then concatenate the decimal string of
// each result. Not base32 — the key is the literal ASCII digit-string bytes.
func spotifyTOTPKey(secret string) []byte {
	var b strings.Builder
	for i := 0; i < len(secret); i++ {
		b.WriteString(strconv.Itoa(int(secret[i]) ^ (i%33 + 9)))
	}
	return []byte(b.String())
}

// spotifyTOTPGenerate computes the 6-digit TOTP code for secret at the given
// unix-second timestamp: standard RFC 4226/6238 HMAC-SHA1 dynamic truncation
// over a 30s time-step counter, using the nonstandard key from
// spotifyTOTPKey.
func spotifyTOTPGenerate(secret string, unixSeconds int64) string {
	key := spotifyTOTPKey(secret)
	counter := uint64(unixSeconds) / spotifyTOTPPeriod

	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(counterBytes[:])
	digest := mac.Sum(nil)

	offset := digest[len(digest)-1] & 0x0F
	code := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7FFFFFFF

	mod := uint32(1)
	for i := 0; i < spotifyTOTPDigits; i++ {
		mod *= 10
	}
	return fmtPad(int(code%mod), spotifyTOTPDigits)
}

// fmtPad zero-pads n to width digits (strconv-only, no fmt import needed
// just for this).
func fmtPad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
