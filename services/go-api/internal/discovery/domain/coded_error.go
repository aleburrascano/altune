package domain

import (
	"fmt"
	"net/http"
)

type CodedError struct {
	Msg    string
	Status int
	Code   string
}

func (e *CodedError) Error() string     { return e.Msg }
func (e *CodedError) HTTPStatus() int   { return e.Status }
func (e *CodedError) ErrorCode() string { return e.Code }

var ErrRateLimited = &CodedError{Msg: "too many requests, try again later", Status: http.StatusTooManyRequests, Code: "discovery.rate_limited"}

func NewInvalidFavoriteError(msg string) *CodedError {
	return &CodedError{Msg: msg, Status: http.StatusBadRequest, Code: "discovery.invalid_favorite"}
}

func NewFavoritesFullError(limit int) *CodedError {
	return &CodedError{Msg: fmt.Sprintf("favorites are full: at most %d per account", limit), Status: http.StatusConflict, Code: "discovery.favorites_full"}
}

func NewInvalidEventError(msg string) *CodedError {
	return &CodedError{Msg: msg, Status: http.StatusBadRequest, Code: "discovery.invalid_event"}
}
