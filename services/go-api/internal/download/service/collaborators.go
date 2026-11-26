package service

import (
	"errors"
	"fmt"
)

var ErrMissingCollaborator = errors.New("a required collaborator is missing")

func missingPort(owner, name string, missing bool) error {
	if !missing {
		return nil
	}
	return fmt.Errorf("%s: %s: %w; pass an explicit adapter when the wiring has none", owner, name, ErrMissingCollaborator)
}
