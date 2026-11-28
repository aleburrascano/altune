package service

import (
	"altune/go-api/internal/search/internal/domain"
	"fmt"
)

type collaborator struct {
	name    string
	missing bool
}

func requireCollaborators(service string, collaborators ...collaborator) error {
	for _, c := range collaborators {
		if c.missing {
			return fmt.Errorf("%w: %s needs %s", domain.ErrArtistPageCollaboratorRequired, service, c.name)
		}
	}
	return nil
}
