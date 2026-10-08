package shared

import "github.com/google/uuid"

func RandomID() string { return uuid.NewString() }
