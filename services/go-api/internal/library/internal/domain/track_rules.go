package domain

const (
	MaxTrackNumber             = 2147483647
	MaxIdempotencyKeyLength    = 200
	MaxFeaturedArtistsPerTrack = 100
)

func ValidateTrackNumber(n int) error {
	if n <= 0 {
		return NewValidationError("track_number must be positive")
	}
	if n > MaxTrackNumber {
		return NewValidationError("track_number exceeds maximum (int4)")
	}
	return nil
}

func ValidateIdempotencyKey(key string) error {
	if key == "" {
		return NewValidationError("idempotency_key must not be empty")
	}
	if err := ValidateText(key, "idempotency_key"); err != nil {
		return err
	}
	if len(key) > MaxIdempotencyKeyLength {
		return NewValidationError("idempotency_key exceeds maximum length")
	}
	return nil
}
