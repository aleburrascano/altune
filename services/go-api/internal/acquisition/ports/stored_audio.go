package ports

import "context"

type StoredAudioReader interface {
	CopyStored(ctx context.Context, audioRef, dir string) (path string, err error)
}
