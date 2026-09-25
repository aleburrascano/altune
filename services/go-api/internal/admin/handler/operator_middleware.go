package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared/httputil"
	"context"
	"log/slog"
	"net/http"
)

func logAdminDenial(r *http.Request, actor string, denial error) {
	code := ""
	if coded, ok := denial.(httputil.ErrorCoder); ok {
		code = coded.ErrorCode()
	}
	slog.WarnContext(r.Context(), "admin.access_denied",
		slog.String("actor", actor),
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.String("code", code),
	)
}

// OperatorOnly admits the operator principal on every method and nobody else.
// It is the admin gate with no read-only principal configured.
func OperatorOnly(operatorUserID string) func(http.Handler) http.Handler {
	return OperatorOrReadOnly(operatorUserID, "")
}

// OperatorOrReadOnly gates the admin tree on two principals: the operator, who
// reaches every route, and the read-only principal (Overseer), who reaches GET
// and nothing else — so a leaked read-only credential cannot pause a loop, flip
// a job or drive a re-run. The verb, not the route table, is what bounds it, so
// a mutating route added later is out of its reach without a second edit.
func OperatorOrReadOnly(operatorUserID, readOnlyUserID string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := auth.RequireUserID(w, r)
			if !ok {
				return
			}
			if err := adminDenial(userID.String(), r.Method, operatorUserID, readOnlyUserID); err != nil {
				logAdminDenial(r, userID.String(), err)
				httputil.HandleServiceError(w, r, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// adminDenial is the whole admin authorization rule — subject and verb — as one
// pure decision: nil admits, a non-nil coded error is the 403 to answer with. A
// blank configured id matches nobody, so an unset OPERATOR_USER_ID or
// OPERATOR_READONLY_USER_ID fails closed rather than admitting every caller.
func adminDenial(userID, method, operatorUserID, readOnlyUserID string) error {
	if operatorUserID != "" && userID == operatorUserID {
		return nil
	}
	if readOnlyUserID == "" || userID != readOnlyUserID {
		return errOperatorRequired
	}
	if method != http.MethodGet {
		return errReadOnlyForbidden
	}
	return nil
}

// operatorActor names the admitted principal for an audit record. OperatorOnly
// guarantees a user id upstream; "unknown" keeps a mis-wired route visible.
func operatorActor(ctx context.Context) string {
	if id, ok := auth.UserIDFromContext(ctx); ok {
		return id.String()
	}
	return "unknown"
}
