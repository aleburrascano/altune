package goapihttp

import (
	"encoding/json"
	"errors"
	"io"
)

var ErrBodyTooLarge = errors.New("goapihttp: response body is larger than its limit")

func DecodeCapped(body io.Reader, limit int64, out any) error {
	capped := &io.LimitedReader{R: body, N: limit + 1}
	if err := json.NewDecoder(capped).Decode(out); err != nil {
		if capped.N <= 0 {
			return ErrBodyTooLarge
		}
		return err
	}
	return nil
}
