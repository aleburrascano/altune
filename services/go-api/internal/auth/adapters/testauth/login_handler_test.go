package testauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginHandler_IssuesAVerifiableTestUserToken(t *testing.T) {
	ta, err := New()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	ta.LoginHandler()(rec, httptest.NewRequest(http.MethodPost, "/test/login", nil))

	var body loginResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d body %q err %v, want a 200 JSON login", rec.Code, rec.Body.String(), err)
	}
	verified, err := ta.Verify(t.Context(), body.AccessToken)
	if err != nil || body.TokenType != "bearer" || body.UserID != TestUserId().String() || verified.UserID != TestUserId() {
		t.Fatalf("login = %+v, verify err %v, want a bearer token for the test user", body, err)
	}
}
