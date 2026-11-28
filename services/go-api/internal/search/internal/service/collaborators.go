package service

import (
	"errors"
	"fmt"
)

var ErrArtistPageCollaboratorRequired = errors.New("artist page services need every collaborator, pass the null adapter to switch one off")

type collaborator struct {
	name    string
	missing bool
}

func requireCollaborators(service string, collaborators ...collaborator) error {
	for _, c := range collaborators {
		if c.missing {
			return fmt.Errorf("%w: %s needs %s", ErrArtistPageCollaboratorRequired, service, c.name)
		}
	}
	return nil
}
