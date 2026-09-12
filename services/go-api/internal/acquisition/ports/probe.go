package ports

import "context"

type AudioProber interface {
	ProbeDuration(ctx context.Context, filePath string) (float64, error)
	ValidateDecodable(ctx context.Context, filePath string) error
}
