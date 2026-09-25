package handler

import (
	"net/http"
	"strings"
	"testing"
)

func TestHandleSuggest_OverlongQ(t *testing.T) {
	router := buildSuggestRouter(&fakeVocabStore{})
	rec := discServe(t, router, http.MethodGet, "/discovery/suggest?q="+strings.Repeat("a", 201), nil)
	discAssertStatus(t, rec, http.StatusBadRequest)
}
