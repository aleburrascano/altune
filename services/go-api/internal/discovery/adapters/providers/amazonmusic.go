package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"

	"github.com/google/uuid"
)

type AmazonMusicAdapter struct {
	client    *http.Client
	resolver  *amazonMusicSessionResolver
	searchURL string
}

const (
	amzSearchURL       = "https://na.web.skill.music.a2z.com/api/showSearch"
	amzSearchTimeout   = 4 * time.Second
	amzUserAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	amzResponseBodyCap = 16 << 20
)

func NewAmazonMusicAdapter(client *http.Client) *AmazonMusicAdapter {
	return &AmazonMusicAdapter{
		client:    client,
		resolver:  newAmazonMusicSessionResolver(client),
		searchURL: amzSearchURL,
	}
}

func (a *AmazonMusicAdapter) Name() domain.ProviderName { return domain.ProviderAmazonMusic }

func (a *AmazonMusicAdapter) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func (a *AmazonMusicAdapter) SearchTimeout() time.Duration { return amzSearchTimeout }

func (a *AmazonMusicAdapter) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	sess, err := a.resolver.get(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve amazon music session: %w", err)
	}

	items, status, err := a.doSearch(ctx, sess, query)
	if err != nil && isAuthStatus(status) {
		a.resolver.invalidate(sess)
		sess, err = a.resolver.get(ctx)
		if err != nil {
			return nil, fmt.Errorf("re-resolve amazon music session: %w", err)
		}
		items, _, err = a.doSearch(ctx, sess, query)
	}
	if err != nil {
		return nil, err
	}

	results := make([]domain.SearchResult, 0, len(items))
	for _, r := range items {
		if kinds[r.Kind] {
			results = append(results, r)
		}
	}
	return results, nil
}

func (a *AmazonMusicAdapter) doSearch(ctx context.Context, sess *amazonMusicSession, query string) ([]domain.SearchResult, int, error) {
	reqBody, err := buildAmazonMusicSearchBody(sess, query)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.searchURL, strings.NewReader(reqBody))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("User-Agent", amzUserAgent)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, amzResponseBodyCap))
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("http status %d", resp.StatusCode)
	}
	if readErr != nil {
		return nil, resp.StatusCode, readErr
	}

	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("decode showSearch response: %w", err)
	}

	seen := map[string]bool{}
	var results []domain.SearchResult
	walkAmazonMusicNode(root, seen, &results)
	return results, resp.StatusCode, nil
}

type amzSearchRequest struct {
	Filter           string `json:"filter"`
	Keyword          string `json:"keyword"`
	SuggestedKeyword string `json:"suggestedKeyword"`
	UserHash         string `json:"userHash"`
	Headers          string `json:"headers"`
}

type amzKeywordElement struct {
	Interface string `json:"interface"`
	Keyword   string `json:"keyword"`
}

type amzAuthElement struct {
	Interface   string `json:"interface"`
	AccessToken string `json:"accessToken"`
}

type amzCSRFElement struct {
	Interface string `json:"interface"`
	Token     string `json:"token"`
	Timestamp string `json:"timestamp"`
	RndNonce  string `json:"rndNonce"`
}

type amzHeadersBundle struct {
	Authentication  string `json:"x-amzn-authentication"`
	DeviceModel     string `json:"x-amzn-device-model"`
	DeviceWidth     string `json:"x-amzn-device-width"`
	DeviceFamily    string `json:"x-amzn-device-family"`
	DeviceID        string `json:"x-amzn-device-id"`
	UserAgent       string `json:"x-amzn-user-agent"`
	SessionID       string `json:"x-amzn-session-id"`
	DeviceHeight    string `json:"x-amzn-device-height"`
	RequestID       string `json:"x-amzn-request-id"`
	DeviceLanguage  string `json:"x-amzn-device-language"`
	CurrencyOfPref  string `json:"x-amzn-currency-of-preference"`
	OSVersion       string `json:"x-amzn-os-version"`
	AppVersion      string `json:"x-amzn-application-version"`
	DeviceTimeZone  string `json:"x-amzn-device-time-zone"`
	Timestamp       string `json:"x-amzn-timestamp"`
	CSRF            string `json:"x-amzn-csrf"`
	MusicDomain     string `json:"x-amzn-music-domain"`
	Referer         string `json:"x-amzn-referer"`
	AffiliateTags   string `json:"x-amzn-affiliate-tags"`
	RefMarker       string `json:"x-amzn-ref-marker"`
	PageURL         string `json:"x-amzn-page-url"`
	WeblabOverrides string `json:"x-amzn-weblab-id-overrides"`
	VideoPlayerTok  string `json:"x-amzn-video-player-token"`
	FeatureFlags    string `json:"x-amzn-feature-flags"`
	HasProfileID    string `json:"x-amzn-has-profile-id"`
	AgeBand         string `json:"x-amzn-age-band"`
}

func buildAmazonMusicSearchBody(sess *amazonMusicSession, query string) (string, error) {
	auth, err := json.Marshal(amzAuthElement{
		Interface: "ClientAuthenticationInterface.v1_0.ClientTokenElement",
	})
	if err != nil {
		return "", err
	}
	csrf, err := json.Marshal(amzCSRFElement{
		Interface: "CSRFInterface.v1_0.CSRFHeaderElement",
		Token:     sess.CSRF.Token,
		Timestamp: sess.CSRF.Ts,
		RndNonce:  sess.CSRF.Rnd,
	})
	if err != nil {
		return "", err
	}
	keyword, err := json.Marshal(amzKeywordElement{
		Interface: "Web.TemplatesInterface.v1_0.Touch.SearchTemplateInterface.SearchKeywordClientInformation",
	})
	if err != nil {
		return "", err
	}

	headers := amzHeadersBundle{
		Authentication: string(auth),
		DeviceModel:    "WEBPLAYER",
		DeviceWidth:    "1920",
		DeviceFamily:   "WebPlayer",
		DeviceID:       sess.DeviceID,
		UserAgent:      amzUserAgent,
		SessionID:      sess.SessionID,
		DeviceHeight:   "1080",
		RequestID:      uuid.NewString(),
		DeviceLanguage: "en_US",
		CurrencyOfPref: "USD",
		OSVersion:      "1.0",
		AppVersion:     sess.Version,
		DeviceTimeZone: "America/New_York",
		Timestamp:      strconv.FormatInt(time.Now().UnixMilli(), 10),
		CSRF:           string(csrf),
		MusicDomain:    "music.amazon.com",
		PageURL:        "https://music.amazon.com/search/" + url.PathEscape(query),
	}
	headersJSON, err := json.Marshal(headers)
	if err != nil {
		return "", err
	}

	out, err := json.Marshal(amzSearchRequest{
		Filter:           `{"IsLibrary":["false"]}`,
		Keyword:          string(keyword),
		SuggestedKeyword: query,
		UserHash:         `{"level":"LIBRARY_MEMBER"}`,
		Headers:          string(headersJSON),
	})
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func walkAmazonMusicNode(node any, seen map[string]bool, out *[]domain.SearchResult) {
	switch v := node.(type) {
	case map[string]any:
		if r, ok := mapAmazonMusicItem(v); ok {
			key := r.Kind.String() + ":" + amazonMusicResultID(r)
			if !seen[key] {
				seen[key] = true
				*out = append(*out, r)
			}
		}
		for _, child := range v {
			walkAmazonMusicNode(child, seen, out)
		}
	case []any:
		for _, child := range v {
			walkAmazonMusicNode(child, seen, out)
		}
	}
}

func amazonMusicResultID(r domain.SearchResult) string {
	if len(r.Sources) > 0 {
		return r.Sources[0].ExternalID
	}
	return ""
}

func isAmazonMusicCardInterface(iface string) bool {
	return strings.HasSuffix(iface, "SquareHorizontalItemElement") ||
		strings.HasSuffix(iface, "SquareVerticalItemElement") ||
		strings.HasSuffix(iface, "CircleVerticalItemElement")
}

func mapAmazonMusicItem(obj map[string]any) (domain.SearchResult, bool) {
	iface, _ := obj["interface"].(string)
	if !isAmazonMusicCardInterface(iface) {
		return domain.SearchResult{}, false
	}

	title := amazonMusicText(obj["primaryText"])
	if strings.TrimSpace(title) == "" {
		return domain.SearchResult{}, false
	}

	kind, externalID, albumASIN, ok := amazonMusicItemIdentity(obj)
	if !ok {
		return domain.SearchResult{}, false
	}

	subtitle := amazonMusicText(obj["secondaryText"])
	image, _ := obj["image"].(string)

	extras := map[string]any{}
	if albumASIN != "" {
		extras["album_asin"] = albumASIN
	}
	if artistASIN := amazonMusicSecondaryArtistASIN(obj); artistASIN != "" && kind != domain.ResultKindArtist {
		extras["artist_asin"] = artistASIN
	}
	if len(extras) == 0 {
		extras = nil
	}

	return domain.NewProviderResult(kind, title, subtitle, image,
		domain.SourceRef{Provider: domain.ProviderAmazonMusic, ExternalID: externalID},
		extras), true
}

func amazonMusicItemIdentity(obj map[string]any) (kind domain.ResultKind, externalID, albumASIN string, ok bool) {
	link, ok2 := obj["primaryLink"].(map[string]any)
	if !ok2 {
		return domain.ResultKindUnknown, "", "", false
	}
	deeplink, _ := link["deeplink"].(string)

	if id, ok3 := amazonMusicDeeplinkID(deeplink, "/artists/"); ok3 {
		return domain.ResultKindArtist, id, "", true
	}
	if album, track, ok3 := amazonMusicAlbumDeeplink(deeplink); ok3 {
		if track != "" {
			return domain.ResultKindTrack, track, album, true
		}
		return domain.ResultKindAlbum, album, "", true
	}
	return domain.ResultKindUnknown, "", "", false
}

func amazonMusicDeeplinkID(deeplink, prefix string) (string, bool) {
	rest, ok := strings.CutPrefix(deeplink, prefix)
	if !ok || rest == "" {
		return "", false
	}
	rest, _, _ = strings.Cut(rest, "?")
	id, _, _ := strings.Cut(rest, "/")
	if id == "" {
		return "", false
	}
	return id, true
}

func amazonMusicAlbumDeeplink(deeplink string) (albumID, trackID string, ok bool) {
	rest, ok := strings.CutPrefix(deeplink, "/albums/")
	if !ok || rest == "" {
		return "", "", false
	}
	path, query, _ := strings.Cut(rest, "?")
	albumID, _, _ = strings.Cut(path, "/")
	if albumID == "" {
		return "", "", false
	}
	if query != "" {
		if values, err := url.ParseQuery(query); err == nil {
			trackID = values.Get("trackAsin")
		}
	}
	return albumID, trackID, true
}

func amazonMusicSecondaryArtistASIN(obj map[string]any) string {
	link, ok := obj["secondaryLink"].(map[string]any)
	if !ok {
		return ""
	}
	deeplink, _ := link["deeplink"].(string)
	id, _ := amazonMusicDeeplinkID(deeplink, "/artists/")
	return id
}

func amazonMusicText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if s, ok := t["text"].(string); ok {
			return s
		}
	}
	return ""
}

func (*AmazonMusicAdapter) ArtworkSource() string { return "amazonmusic" }
