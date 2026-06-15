package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// --- helpers ----------------------------------------------------------------

type s3Env struct {
	endpoint  string
	accessKey string
	secretKey string
	bucket    string
	region    string
}

func testS3Env(t *testing.T) s3Env {
	t.Helper()
	endpoint := os.Getenv("OCI_S3_ENDPOINT")
	accessKey := os.Getenv("OCI_S3_ACCESS_KEY")
	secretKey := os.Getenv("OCI_S3_SECRET_KEY")
	bucket := os.Getenv("OCI_S3_BUCKET")
	region := os.Getenv("OCI_S3_REGION")

	if endpoint == "" || accessKey == "" || secretKey == "" || bucket == "" {
		t.Skip("OCI_S3_* env vars not set, skipping S3 integration test")
	}

	return s3Env{
		endpoint:  endpoint,
		accessKey: accessKey,
		secretKey: secretKey,
		bucket:    bucket,
		region:    region,
	}
}

func testObjectStore(t *testing.T) *ObjectStorageAudioStore {
	t.Helper()
	env := testS3Env(t)
	store, err := NewObjectStorageAudioStore(env.endpoint, env.accessKey, env.secretKey, env.bucket, env.region)
	if err != nil {
		t.Fatalf("create ObjectStorageAudioStore: %v", err)
	}
	return store
}

func testAudioRef(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("integration-test/%s.opus", t.Name())
}

// --- ObjectStorageAudioStore ------------------------------------------------

func TestObjectStorageAudioStore_StoreAndStream(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()
	audioRef := testAudioRef(t)

	// Cleanup after test
	t.Cleanup(func() {
		_ = store.Delete(context.Background(), audioRef)
	})

	// Arrange: write a temp source file
	content := []byte("fake audio data for object-storage store-and-stream test")
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.opus")
	if err := os.WriteFile(srcPath, content, 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	// Act: store
	if err := store.Store(ctx, srcPath, audioRef); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Act: stream back
	rc, size, err := store.Stream(ctx, audioRef)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer rc.Close()

	// Assert: content matches
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("streamed content mismatch: got %d bytes, want %d bytes", len(got), len(content))
	}
	if size != int64(len(content)) {
		t.Errorf("streamed size: got %d, want %d", size, len(content))
	}
}

func TestObjectStorageAudioStore_Exists(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()
	audioRef := testAudioRef(t)

	t.Cleanup(func() {
		_ = store.Delete(context.Background(), audioRef)
	})

	// Arrange: store a file
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.opus")
	if err := os.WriteFile(srcPath, []byte("exists-test-data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := store.Store(ctx, srcPath, audioRef); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Assert: stored object exists
	exists, err := store.Exists(ctx, audioRef)
	if err != nil {
		t.Fatalf("Exists (stored): %v", err)
	}
	if !exists {
		t.Error("expected stored object to exist, got false")
	}

	// Assert: non-existent object returns false
	exists, err = store.Exists(ctx, "integration-test/no-such-file-ever.opus")
	if err != nil {
		t.Fatalf("Exists (missing): %v", err)
	}
	if exists {
		t.Error("expected non-existent object to return false, got true")
	}
}

func TestObjectStorageAudioStore_Delete(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()
	audioRef := testAudioRef(t)

	// Arrange: store a file
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.opus")
	if err := os.WriteFile(srcPath, []byte("to-delete-object"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := store.Store(ctx, srcPath, audioRef); err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Act: delete
	if err := store.Delete(ctx, audioRef); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Assert: no longer exists
	exists, err := store.Exists(ctx, audioRef)
	if err != nil {
		t.Fatalf("Exists after delete: %v", err)
	}
	if exists {
		t.Error("expected object to not exist after delete, got true")
	}
}

func TestObjectStorageAudioStore_Stream_NotFound(t *testing.T) {
	store := testObjectStore(t)
	ctx := context.Background()

	rc, _, err := store.Stream(ctx, "integration-test/nonexistent-stream-test.opus")
	if err == nil {
		rc.Close()
		t.Fatal("expected error streaming non-existent object, got nil")
	}
}
