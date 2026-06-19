package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"

	"altune/go-api/internal/discovery/domain"
)

type YouTubeArtworkResolver struct {
	client *http.Client
	apiKey string
}

func NewYouTubeArtworkResolver(client *http.Client, apiKey string) *YouTubeArtworkResolver {
	return &YouTubeArtworkResolver{client: client, apiKey: apiKey}
}

func (a *YouTubeArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if kind != domain.ResultKindArtist {
		return "", nil
	}

	query := buildYouTubeQuery(title, subtitle)
	channelID, err := a.searchChannel(ctx, query)
	if err != nil || channelID == "" {
		return "", nil
	}

	thumbnail, err := a.fetchChannelThumbnail(ctx, channelID)
	if err != nil || thumbnail == "" {
		return "", nil
	}

	slog.DebugContext(ctx, "youtube.artwork_resolved",
		"title", title, "channel_id", channelID)
	return thumbnail, nil
}

func buildYouTubeQuery(title, subtitle string) string {
	if subtitle != "" {
		return fmt.Sprintf(`"%s" "%s"`, title, subtitle)
	}
	return fmt.Sprintf(`"%s" music`, title)
}

type ytSearchResponse struct {
	Items []ytSearchItem `json:"items"`
}

type ytSearchItem struct {
	ID ytSearchID `json:"id"`
}

type ytSearchID struct {
	ChannelID string `json:"channelId"`
}

type ytChannelResponse struct {
	Items []ytChannelItem `json:"items"`
}

type ytChannelItem struct {
	Snippet ytChannelSnippet `json:"snippet"`
}

type ytChannelSnippet struct {
	Thumbnails ytThumbnails `json:"thumbnails"`
}

type ytThumbnails struct {
	Default ytThumbnail `json:"default"`
	Medium  ytThumbnail `json:"medium"`
	High    ytThumbnail `json:"high"`
}

type ytThumbnail struct {
	URL string `json:"url"`
}

func (a *YouTubeArtworkResolver) searchChannel(ctx context.Context, query string) (string, error) {
	u := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/search?part=snippet&type=channel&maxResults=1&q=%s&key=%s",
		url.QueryEscape(query), url.QueryEscape(a.apiKey))

	body, err := a.doGet(ctx, u)
	if err != nil {
		return "", err
	}

	var resp ytSearchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	if len(resp.Items) == 0 {
		return "", nil
	}
	return resp.Items[0].ID.ChannelID, nil
}

func (a *YouTubeArtworkResolver) fetchChannelThumbnail(ctx context.Context, channelID string) (string, error) {
	u := fmt.Sprintf(
		"https://www.googleapis.com/youtube/v3/channels?part=snippet&id=%s&key=%s",
		url.QueryEscape(channelID), url.QueryEscape(a.apiKey))

	body, err := a.doGet(ctx, u)
	if err != nil {
		return "", err
	}

	var resp ytChannelResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	if len(resp.Items) == 0 {
		return "", nil
	}

	thumbs := resp.Items[0].Snippet.Thumbnails
	if thumbs.High.URL != "" {
		return thumbs.High.URL, nil
	}
	if thumbs.Medium.URL != "" {
		return thumbs.Medium.URL, nil
	}
	return thumbs.Default.URL, nil
}

func (a *YouTubeArtworkResolver) doGet(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		slog.WarnContext(ctx, "youtube.api_error",
			"status", resp.StatusCode, "url", rawURL)
		return nil, fmt.Errorf("youtube returned %d", resp.StatusCode)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}
