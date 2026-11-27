package httpuser

import (
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/requestuser/internal/core/signedinuser"
	"errors"
	"net/http"
)

var ErrNoSignedInUser = errors.New("requestuser: no signed-in user on a route that requires one")

func UserID(r *http.Request) ids.UserId {
	id, ok := signedinuser.UserIDFromContext(r.Context())
	if !ok {
		panic(ErrNoSignedInUser)
	}
	return id
}
