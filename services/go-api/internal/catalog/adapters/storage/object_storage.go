package storage

import (
	"altune/go-api/internal/catalog/ports"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	_ ports.AudioStore     = (*ObjectStorageAudioStore)(nil)
	_ ports.AudioURLSigner = (*ObjectStorageAudioStore)(nil)
	_ ports.AudioLister    = (*ObjectStorageAudioStore)(nil)
)

// storageOpTimeout bounds a single control-plane object-storage round-trip
// (stat, list, delete) — operations whose duration does not depend on payload
// size. Derived from the caller's context so a shorter caller deadline still
// wins, it caps the worst case so a wedged S3 endpoint cannot block the handler
// goroutine indefinitely. Thirty seconds mirrors the transport's existing
// ResponseHeaderTimeout; it is a deliberate default, not a tuned one (#426).
//
// The two bulk-transfer operations are deliberately NOT wrapped: Stream is the
// long-lived audio-streaming path (a fixed deadline would truncate a legitimate
// long playback), and Store uploads an arbitrarily large file. Both instead
// inherit the caller's own request budget plus the transport-level dial, TLS and
// response-header timeouts configured in NewObjectStorageAudioStore.
const storageOpTimeout = 30 * time.Second

type ObjectStorageAudioStore struct {
	client *minio.Client
	bucket string
}

func NewObjectStorageAudioStore(endpoint, accessKey, secretKey, bucket, region string) (*ObjectStorageAudioStore, error) {
	secure := true
	host := endpoint
	if strings.HasPrefix(host, "https://") {
		host = strings.TrimPrefix(host, "https://")
	} else if strings.HasPrefix(host, "http://") {
		host = strings.TrimPrefix(host, "http://")
		secure = false
	}
	host = strings.TrimRight(host, "/")

	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   10,
	}

	client, err := minio.New(host, &minio.Options{
		Creds:     credentials.NewStaticV4(accessKey, secretKey, ""),
		Region:    region,
		Secure:    secure,
		Transport: transport,
	})
	if err != nil {
		return nil, fmt.Errorf("create s3 client: %w", err)
	}
	return &ObjectStorageAudioStore{client: client, bucket: bucket}, nil
}

func (s *ObjectStorageAudioStore) Exists(ctx context.Context, audioRef string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, storageOpTimeout)
	defer cancel()

	_, err := s.client.StatObject(ctx, s.bucket, audioRef, minio.StatObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *ObjectStorageAudioStore) List(ctx context.Context, prefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, storageOpTimeout)
	defer cancel()

	var refs []string
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list %q: %w", prefix, obj.Err)
		}
		refs = append(refs, obj.Key)
	}
	return refs, nil
}

func (s *ObjectStorageAudioStore) Store(ctx context.Context, sourcePath string, audioRef string) error {
	file, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat source file: %w", err)
	}

	_, err = s.client.PutObject(ctx, s.bucket, audioRef, file, stat.Size(), minio.PutObjectOptions{
		ContentType: ports.AudioContentType(audioRef),
	})
	if err != nil {
		return fmt.Errorf("upload to s3: %w", err)
	}
	return nil
}

func (s *ObjectStorageAudioStore) Stream(ctx context.Context, audioRef string) (ports.AudioStream, int64, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, audioRef, minio.GetObjectOptions{})
	if err != nil {
		return nil, 0, fmt.Errorf("get object: %w", err)
	}

	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, 0, fmt.Errorf("stat object: %w", err)
	}

	return obj, stat.Size, nil
}

func (s *ObjectStorageAudioStore) PresignGet(ctx context.Context, audioRef string, ttl time.Duration) (string, error) {
	u, err := s.client.PresignedGetObject(ctx, s.bucket, audioRef, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("presign get %q: %w", audioRef, err)
	}
	return u.String(), nil
}

func (s *ObjectStorageAudioStore) Delete(ctx context.Context, audioRef string) error {
	ctx, cancel := context.WithTimeout(ctx, storageOpTimeout)
	defer cancel()

	return s.client.RemoveObject(ctx, s.bucket, audioRef, minio.RemoveObjectOptions{})
}
