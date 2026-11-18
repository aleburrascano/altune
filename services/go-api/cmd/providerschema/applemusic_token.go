package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
)

const (
	appleMusicSiteURL    = "https://music.apple.com"
	appleMusicCatalogURL = "https://api.music.apple.com/v1/catalog/us"
	appleMusicUserAgent  = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

var (
	appleMusicBundle = regexp.MustCompile(`assets/index~[A-Za-z0-9]+\.js`)
	appleMusicJWT    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)
)

type appleMusicCatalog struct {
	client     *http.Client
	siteURL    string
	catalogURL string
	token      string
}

func newAppleMusicCatalog(client *http.Client, siteURL, catalogURL string) *appleMusicCatalog {
	return &appleMusicCatalog{client: client, siteURL: siteURL, catalogURL: catalogURL}
}

func (apple *appleMusicCatalog) CatalogResource(ctx context.Context, kind, id string) (json.RawMessage, error) {
	if err := apple.ensureToken(ctx); err != nil {
		return nil, err
	}
	raw, err := apple.get(ctx, fmt.Sprintf("%s/%s/%s", apple.catalogURL, url.PathEscape(kind), url.PathEscape(id)), apple.token)
	if err != nil {
		return nil, err
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("apple music %s %s: %w", kind, id, err)
	}
	if len(body.Data) == 0 {
		return nil, fmt.Errorf("apple music %s %s: no resource in the response", kind, id)
	}
	return body.Data[0], nil
}

func (apple *appleMusicCatalog) get(ctx context.Context, rawURL, token string) ([]byte, error) {
	header := map[string]string{"User-Agent": appleMusicUserAgent, "Origin": apple.siteURL}
	if token != "" {
		header["Authorization"] = "Bearer " + token
	}
	return fetch(ctx, apple.client, request{method: http.MethodGet, url: rawURL, header: header})
}

func (apple *appleMusicCatalog) ensureToken(ctx context.Context) error {
	if apple.token != "" {
		return nil
	}
	page, err := apple.get(ctx, apple.siteURL+"/us/search", "")
	if err != nil {
		return fmt.Errorf("apple music page: %w", err)
	}
	bundle, err := apple.get(ctx, apple.siteURL+"/"+string(appleMusicBundle.Find(page)), "")
	if err != nil {
		return fmt.Errorf("apple music bundle: %w", err)
	}
	apple.token = string(appleMusicJWT.Find(bundle))
	if apple.token == "" {
		return errors.New("no token in the apple music bundle")
	}
	return nil
}
