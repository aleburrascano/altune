package main

import (
	"altune/go-api/internal/shared/providerfields"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
)

const adapterRoot = "services/go-api/internal/search/internal/adapters/providers/"

type provider struct {
	adapter      string
	sharedDirs   []string
	decodedAs    map[string]string
	decodedLater []rawField
	source       func(client *http.Client) (source, error)
}

type rawField struct {
	entity    string
	path      string
	decodedAs string
}

var providers = map[string]provider{
	"applemusic":  appleMusicProvider(),
	"deezer":      deezerProvider(),
	"discogs":     discogsProvider(),
	"fanarttv":    fanartTVProvider(),
	"genius":      geniusProvider(),
	"itunes":      itunesProvider(),
	"musicbrainz": musicBrainzProvider(),
	"soundcloud":  soundCloudProvider(),
	"spotify":     spotifyProvider(),
	"theaudiodb":  theAudioDBProvider(),
	"trackers":    trackersProvider(),
	"ytmusic":     ytMusicProvider(),
}

func schemaPath(root, provider string) string {
	return filepath.Join(root, "docs", "providers", "schema", provider+".yaml")
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("no .git above the working directory")
		}
		dir = parent
	}
}

func account(s providerfields.Schema, root string, p provider, update bool) ([]string, error) {
	reads, err := p.reads(root, s)
	if err != nil {
		return nil, err
	}
	var problems []string
	for name, entity := range s.Entities {
		if update {
			entity.Account(reads[name])
		}
		problems = append(problems, entity.Audit(name, reads[name])...)
	}
	return problems, nil
}

func (p provider) reads(root string, s providerfields.Schema) (map[string]providerfields.Reads, error) {
	if err := sameEntities(s, p); err != nil {
		return nil, err
	}
	out := map[string]providerfields.Reads{}
	for name, typeName := range p.decodedAs {
		reads, err := p.entityReads(root, name, typeName)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out[name] = reads
	}
	return out, nil
}

func (p provider) entityReads(root, entity, typeName string) (providerfields.Reads, error) {
	dirs := p.sourceDirs(root)
	reads, err := providerfields.DecodedFields(dirs, typeName)
	for _, raw := range p.decodedLater {
		if err == nil && raw.entity == entity {
			reads, err = raw.readThrough(reads, dirs)
		}
	}
	return reads, err
}

func (p provider) sourceDirs(root string) []string {
	dirs := []string{filepath.Join(root, p.adapter)}
	for _, dir := range p.sharedDirs {
		dirs = append(dirs, filepath.Join(root, dir))
	}
	return dirs
}

func (raw rawField) readThrough(reads providerfields.Reads, dirs []string) (providerfields.Reads, error) {
	later, err := providerfields.DecodedFields(dirs, raw.decodedAs)
	if err != nil {
		return reads, err
	}
	return reads.Graft(raw.path, later)
}

func sameEntities(s providerfields.Schema, p provider) error {
	inSchema, audited := sortedNames(s.Entities), sortedNames(p.decodedAs)
	if !slices.Equal(inSchema, audited) {
		return fmt.Errorf("the schema has entities %v but the adapter is audited for %v", inSchema, audited)
	}
	return nil
}
