package handler_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
)

func newReqWithUser(t *testing.T, id shared.UserId, withUser bool) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/", nil)
	if withUser {
		req = req.WithContext(auth.ContextWithUserID(req.Context(), id))
	}
	return req
}

func TestOperatorOnly(t *testing.T) {
	operator := shared.NewUserId(uuid.New())
	other := shared.NewUserId(uuid.New())

	tests := []struct {
		name           string
		operatorUserID string
		userID         shared.UserId
		withUser       bool
		wantStatus     int
		wantNext       bool
	}{
		{
			name:           "operator account passes through",
			operatorUserID: operator.String(),
			userID:         operator,
			withUser:       true,
			wantStatus:     http.StatusOK,
			wantNext:       true,
		},
		{
			name:           "non-operator account is forbidden",
			operatorUserID: operator.String(),
			userID:         other,
			withUser:       true,
			wantStatus:     http.StatusForbidden,
			wantNext:       false,
		},
		{
			name:           "unauthenticated request is rejected before the operator check",
			operatorUserID: operator.String(),
			withUser:       false,
			wantStatus:     http.StatusUnauthorized,
			wantNext:       false,
		},
		{
			name:           "unset operator id fails closed even with a valid user",
			operatorUserID: "",
			userID:         operator,
			withUser:       true,
			wantStatus:     http.StatusForbidden,
			wantNext:       false,
		},
		{
			name:           "zero-value user id with unset config is denied",
			operatorUserID: "",
			userID:         shared.UserId{},
			withUser:       true,
			wantStatus:     http.StatusForbidden,
			wantNext:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nextCalled := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextCalled = true
				w.WriteHeader(http.StatusOK)
			})

			rec := httptest.NewRecorder()
			req := newReqWithUser(t, tt.userID, tt.withUser)
			handler.OperatorOnly(tt.operatorUserID)(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if nextCalled != tt.wantNext {
				t.Errorf("next called = %v, want %v", nextCalled, tt.wantNext)
			}
		})
	}
}
