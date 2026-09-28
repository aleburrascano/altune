package goapi

import "context"

type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type StaticTokenSource string

func (s StaticTokenSource) Token(context.Context) (string, error) {
	if s == "" {
		return "", ErrNoToken
	}
	return string(s), nil
}
