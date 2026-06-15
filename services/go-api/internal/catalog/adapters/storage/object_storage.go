package storage

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

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
		TLSHandshakeTimeout:  10 * time.Second,
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
		ContentType: "audio/mpeg",
	})
	if err != nil {
		return fmt.Errorf("upload to s3: %w", err)
	}
	return nil
}

func (s *ObjectStorageAudioStore) Stream(ctx context.Context, audioRef string) (io.ReadCloser, int64, error) {
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

func (s *ObjectStorageAudioStore) Delete(ctx context.Context, audioRef string) error {
	return s.client.RemoveObject(ctx, s.bucket, audioRef, minio.RemoveObjectOptions{})
}
