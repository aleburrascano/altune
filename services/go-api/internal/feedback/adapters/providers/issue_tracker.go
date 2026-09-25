package providers

import (
	"altune/go-api/internal/feedback/domain"
	"altune/go-api/internal/feedback/ports"
	"altune/go-api/internal/shared/logging"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.github.com"
	apiVersion     = "2022-11-28"
	requestTimeout = 15 * time.Second
	maxErrorBody   = 4 << 10
	maxIssueBody   = 1 << 20
	sourceLabel    = "from-app"
	// errPrefix labels every failure from the issue-creation call chain so the
	// wording stays identical across branches and cannot drift again.
	errPrefix = "github issues"
)

// wrapErr prefixes err with errPrefix while preserving its wrapped chain, so
// every failure branch of Create reports through one shared mechanism.
func wrapErr(err error) error {
	return fmt.Errorf("%s: %w", errPrefix, err)
}

type GitHubIssueTracker struct {
	client  *http.Client
	baseURL string
	repo    string
	token   string
}

func NewGitHubIssueTracker(repo, token string) *GitHubIssueTracker {
	return &GitHubIssueTracker{
		client:  &http.Client{Timeout: requestTimeout},
		baseURL: defaultBaseURL,
		repo:    repo,
		token:   token,
	}
}

// kindLabels maps a feedback Kind to the GitHub issue label its issue gets.
// The label vocabulary is GitHub's, so it lives here in the adapter rather than
// in the domain. An undefined kind maps to "", never mislabelling it as a bug.
var kindLabels = map[domain.Kind]string{
	domain.KindBug:       "bug",
	domain.KindIdea:      "enhancement",
	domain.KindConfusing: "ux",
}

func labelFor(kind domain.Kind) string {
	return kindLabels[kind]
}

type createIssueRequest struct {
	Title  string   `json:"title"`
	Body   string   `json:"body"`
	Labels []string `json:"labels"`
}

type createIssueResponse struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
}

func (t *GitHubIssueTracker) Create(ctx context.Context, report *domain.Report) (ports.IssueRef, error) {
	req, err := t.newRequest(ctx, report)
	if err != nil {
		return ports.IssueRef{}, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return ports.IssueRef{}, transportError(err)
	}
	defer resp.Body.Close()
	defer drain(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		return ports.IssueRef{}, statusError(resp, time.Now())
	}
	return t.readCreated(ctx, resp)
}

// readCreated decodes the issue GitHub confirmed with a 201. A read or decode
// failure here is NOT a non-created issue: GitHub already wrote it, we merely
// lost the confirmation. It is logged distinctly (status + raw body) so ops can
// tell it apart from a true creation failure, then the error still propagates —
// the caller must not blindly retry, which would create a real duplicate (#589).
func (t *GitHubIssueTracker) readCreated(ctx context.Context, resp *http.Response) (ports.IssueRef, error) {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxIssueBody))
	if err != nil {
		return ports.IssueRef{}, outcomeUnknown(confirmedButUndecoded(ctx, resp.StatusCode, raw, wrapErr(fmt.Errorf("read issue: %w", err))))
	}
	ref, err := decodeIssue(raw)
	if err != nil {
		return ports.IssueRef{}, outcomeUnknown(confirmedButUndecoded(ctx, resp.StatusCode, raw, err))
	}
	return ref, nil
}

// confirmedButUndecoded logs a confirmed-201-but-undecoded response distinctly,
// carrying the status and the raw body, then returns err unchanged so the HTTP
// response to the caller stays an error.
func confirmedButUndecoded(ctx context.Context, status int, raw []byte, err error) error {
	slog.ErrorContext(ctx, "github.issue_confirmed_but_undecoded",
		"status", status,
		"raw_body", boundedBody(raw),
		"error", err.Error(),
	)
	return err
}

// boundedBody trims the raw body to a log-friendly size so a large or malformed
// confirmation body cannot flood the logs.
func boundedBody(raw []byte) string {
	return strings.TrimSpace(string(raw[:min(len(raw), maxErrorBody)]))
}

// drain discards what is left of a body, bounded, before it is closed so the
// keep-alive connection can be reused instead of torn down.
func drain(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxIssueBody))
}

func (t *GitHubIssueTracker) newRequest(ctx context.Context, report *domain.Report) (*http.Request, error) {
	payload, err := json.Marshal(createIssueRequest{
		Title:  plainTitle(report.Title()),
		Body:   renderBody(report, logging.CorrelationIDFromContext(ctx)),
		Labels: []string{labelFor(report.Kind), sourceLabel},
	})
	if err != nil {
		return nil, fmt.Errorf("encode issue: %w", err)
	}
	url := fmt.Sprintf("%s/repos/%s/issues", t.baseURL, t.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build issue request: %w", err)
	}
	setHeaders(req, t.token)
	return req, nil
}

func setHeaders(req *http.Request, token string) {
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("Content-Type", "application/json")
}

// readErrorBody returns GitHub's error body, bounded and trimmed, for the
// failure message. statusError (in errors.go) turns that into a classified error.
func readErrorBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	return strings.TrimSpace(string(body))
}

func decodeIssue(raw []byte) (ports.IssueRef, error) {
	var created createIssueResponse
	if err := json.Unmarshal(raw, &created); err != nil {
		return ports.IssueRef{}, wrapErr(fmt.Errorf("decode issue: %w", err))
	}
	if created.Number == 0 {
		return ports.IssueRef{}, wrapErr(errors.New("response carried no issue number"))
	}
	return ports.IssueRef{Number: created.Number, URL: created.HTMLURL}, nil
}
