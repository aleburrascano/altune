package httpserver

import (
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"
)

const (
	internalServerErrorDetail = "internal server error"
	problemContentType        = "application/problem+json"
	problemTypePrefix         = "urn:altune:problem:"
	statusClientClosedRequest = 499
)

type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Status   int    `json:"status"`
	Detail   string `json:"detail"`
	Instance string `json:"instance,omitempty"`
	Code     string `json:"code"`
}

type retryAfterer interface {
	RetryAfter() time.Duration
}

var kindStatus = map[apierror.Kind]int{
	apierror.KindInternal:        http.StatusInternalServerError,
	apierror.KindInvalid:         http.StatusBadRequest,
	apierror.KindUnauthenticated: http.StatusUnauthorized,
	apierror.KindForbidden:       http.StatusForbidden,
	apierror.KindNotFound:        http.StatusNotFound,
	apierror.KindConflict:        http.StatusConflict,
	apierror.KindTooLarge:        http.StatusRequestEntityTooLarge,
	apierror.KindRateLimited:     http.StatusTooManyRequests,
	apierror.KindUpstreamFailed:  http.StatusBadGateway,
	apierror.KindUnavailable:     http.StatusServiceUnavailable,
	apierror.KindTimeout:         http.StatusGatewayTimeout,
}

func HandleServiceError(w http.ResponseWriter, r *http.Request, err error) {
	if clientLeft(r, err) {
		slog.InfoContext(r.Context(), "request.cancelled",
			logging.KeyMethod, r.Method, logging.KeyPath, r.URL.Path, logging.KeyError, err)
		w.WriteHeader(statusClientClosedRequest)
		return
	}
	p := problemFor(r, err)
	logFailure(r, p, err)
	setRetryAfter(w.Header(), err)
	writeProblem(w, p)
}

func clientLeft(r *http.Request, err error) bool {
	return errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled)
}

func ranOutOfTime(r *http.Request, err error) bool {
	return errors.Is(err, context.DeadlineExceeded) && errors.Is(r.Context().Err(), context.DeadlineExceeded)
}

func problemFor(r *http.Request, err error) Problem {
	var coded *apierror.CodedError
	if errors.As(err, &coded) {
		return newProblem(r, coded.Code(), coded.Error())
	}
	if ranOutOfTime(r, err) {
		return newProblem(r, apierror.CodeRequestTimeout, "")
	}
	return newProblem(r, apierror.CodeInternal, internalServerErrorDetail)
}

func newProblem(r *http.Request, code apierror.Code, detail string) Problem {
	if detail == "" {
		detail = code.Title()
	}
	return Problem{
		Type:     problemTypePrefix + code.String(),
		Title:    code.Title(),
		Status:   statusOf(code.Kind()),
		Detail:   detail,
		Instance: logging.CorrelationIDFromContext(r.Context()),
		Code:     code.String(),
	}
}

func statusOf(kind apierror.Kind) int {
	if status, ok := kindStatus[kind]; ok {
		return status
	}
	return http.StatusInternalServerError
}

func logFailure(r *http.Request, p Problem, err error) {
	level, message := slog.LevelInfo, "request.rejected"
	switch {
	case p.Code == apierror.CodeRequestTimeout.String():
		level, message = slog.LevelWarn, "request.timed_out"
	case p.Status >= http.StatusInternalServerError:
		level, message = slog.LevelError, "request.failed"
	}
	slog.Log(r.Context(), level, message,
		logging.KeyMethod, r.Method,
		logging.KeyPath, r.URL.Path,
		logging.KeyStatus, p.Status,
		logging.KeyCode, p.Code,
		logging.KeyError, err)
}

func writeProblem(w http.ResponseWriter, p Problem) {
	body, err := json.Marshal(p)
	if err != nil {
		slog.Error("problem.encode_failed", logging.KeyStatus, p.Status, logging.KeyCode, p.Code, logging.KeyError, err)
		w.WriteHeader(p.Status)
		return
	}
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(p.Status)
	_, _ = w.Write(append(body, '\n'))
}

func setRetryAfter(h http.Header, err error) {
	var retryable retryAfterer
	if !errors.As(err, &retryable) || h.Get("Retry-After") != "" {
		return
	}
	if wait := retryable.RetryAfter(); wait > 0 {
		h.Set("Retry-After", RetryAfterSeconds(wait))
	}
}

func RetryAfterSeconds(wait time.Duration) string {
	return strconv.Itoa(max(1, int(math.Ceil(wait.Seconds()))))
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("encode %T response: %v", v, err))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
