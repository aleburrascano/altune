package ports

import "context"

type AudioProber interface {
	// ProbeDuration returns the file's measured length in seconds. A length it
	// could not measure is an error, never a zero: the caller compares the
	// number against the track's expected length, so a zero standing for
	// "unknown" would read as a zero-length file and condemn a good download.
	ProbeDuration(ctx context.Context, filePath string) (float64, error)

	// ValidateDecodable returns an error only for audio the decoder itself
	// refused. A decode that could not be attempted or could not finish is
	// silence, not a verdict, and must not be reported as undecodable.
	ValidateDecodable(ctx context.Context, filePath string) error
}
