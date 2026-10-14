package main

import (
	"context"
	"fmt"
	"image"
	"io"
	"net/http"
	"strings"
	"sync"
)

const maxCoverBytes = 10 << 20

type coverSignature struct {
	hash   uint64
	spread float64
	err    error
}

type coverComparer struct {
	http  *http.Client
	mu    sync.Mutex
	cache map[string]coverSignature
}

func newCoverComparer(client *http.Client) *coverComparer {
	return &coverComparer{http: client, cache: map[string]coverSignature{}}
}

func (c *coverComparer) SameImage(ctx context.Context, libraryURL, catalogURL string) (bool, error) {
	a, err := c.signatureAt(ctx, libraryURL)
	if err != nil {
		return false, err
	}
	b, err := c.signatureAt(ctx, catalogURL)
	if err != nil {
		return false, err
	}
	return isSameImage(a.hash, b.hash), nil
}

func (c *coverComparer) IsBlank(ctx context.Context, url string) (bool, error) {
	signature, err := c.signatureAt(ctx, url)
	if err != nil {
		return false, err
	}
	return isBlank(signature.spread), nil
}

func (c *coverComparer) signatureAt(ctx context.Context, url string) (coverSignature, error) {
	c.mu.Lock()
	cached, ok := c.cache[url]
	c.mu.Unlock()
	if ok {
		return cached, cached.err
	}
	signature := c.fetchSignature(ctx, url)
	c.mu.Lock()
	c.cache[url] = signature
	c.mu.Unlock()
	return signature, signature.err
}

func (c *coverComparer) fetchSignature(ctx context.Context, url string) coverSignature {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return coverSignature{err: fmt.Errorf("build cover request: %w", err)}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return coverSignature{err: fmt.Errorf("fetch cover: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return coverSignature{err: fmt.Errorf("fetch cover: status %d", resp.StatusCode)}
	}
	img, _, err := image.Decode(io.LimitReader(resp.Body, maxCoverBytes))
	if err != nil {
		return coverSignature{err: fmt.Errorf("decode cover: %w", err)}
	}
	hash, spread := signatureOf(img)
	return coverSignature{hash: hash, spread: spread}
}

func coverURLContainsImage(artworkURL, imageMD5 string) bool {
	return imageMD5 != "" && strings.Contains(artworkURL, imageMD5)
}
