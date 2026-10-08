package httputil

import "net/http"

func WritePlainError(w http.ResponseWriter, status int, msg string) {
	http.Error(w, msg, status)
}
