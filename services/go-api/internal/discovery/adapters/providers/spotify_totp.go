package providers

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"strconv"
	"strings"
)

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

func spotifyTOTPKey(secret string) []byte {
	var b strings.Builder
	for i := 0; i < len(secret); i++ {
		b.WriteString(strconv.Itoa(int(secret[i]) ^ (i%33 + 9)))
	}
	return []byte(b.String())
}

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

func fmtPad(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
