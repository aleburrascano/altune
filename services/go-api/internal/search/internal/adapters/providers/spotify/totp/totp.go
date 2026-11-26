package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"strconv"
	"strings"
)

const (
	periodSeconds = 30
	codeDigits    = 6
)

func Code(secret string, unixSeconds int64) string {
	counter := uint64(unixSeconds) / periodSeconds

	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)

	mac := hmac.New(sha1.New, key(secret))
	mac.Write(counterBytes[:])
	digest := mac.Sum(nil)

	offset := digest[len(digest)-1] & 0x0F
	code := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7FFFFFFF

	mod := uint32(1)
	for i := 0; i < codeDigits; i++ {
		mod *= 10
	}
	return zeroPadded(int(code%mod), codeDigits)
}

func key(secret string) []byte {
	var b strings.Builder
	for i := 0; i < len(secret); i++ {
		b.WriteString(strconv.Itoa(int(secret[i]) ^ (i%33 + 9)))
	}
	return []byte(b.String())
}

func zeroPadded(n, width int) string {
	s := strconv.Itoa(n)
	for len(s) < width {
		s = "0" + s
	}
	return s
}
