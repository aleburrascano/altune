package auth

import testauth "altune/go-api/internal/auth/internal/adapters/testauth"

type TestAuth = testauth.TestAuth

var (
	CombineTestVerifier = testauth.Combine
	NewTestAuth         = testauth.New
	TestUserId          = testauth.TestUserId
)
