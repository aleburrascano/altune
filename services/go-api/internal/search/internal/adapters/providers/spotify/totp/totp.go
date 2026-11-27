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

	truncationOffsetMask = 0x0F
	truncatedBytes       = 4
	signBitMask          = 0x7FFFFFFF
	secretXorCycle       = 33
	secretXorBase        = 9
)

func Code(secret string, unixSeconds int64) string {
	counter := uint64(unixSeconds) / periodSeconds

	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)

	mac := hmac.New(sha1.New, key(secret))
	mac.Write(counterBytes[:])
	digest := mac.Sum(nil)

	offset := digest[len(digest)-1] & truncationOffsetMask
	code := binary.BigEndian.Uint32(digest[offset:offset+truncatedBytes]) & signBitMask

	mod := uint32(1)
	for i := 0; i < codeDigits; i++ {
		mod *= 10
	}
	return zeroPadded(int(code%mod), codeDigits)
}

func key(secret string) []byte {
	var b strings.Builder
	for i := 0; i < len(secret); i++ {
		b.WriteString(strconv.Itoa(int(secret[i]) ^ (i%secretXorCycle + secretXorBase)))
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
