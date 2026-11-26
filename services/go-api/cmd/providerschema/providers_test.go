package main

import (
	"altune/go-api/internal/shared/providerfields"
	"flag"
	"strings"
	"testing"
)

var updateSchema = flag.Bool("update-schema", false, "rewrite each schema's read flags from its adapter's decode structs")

func TestEverySchemaAccountsForEveryField(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	for provider, p := range providers {
		path := schemaPath(root, provider)
		s, err := providerfields.Load(path)
		if err != nil {
			t.Fatal(err)
		}
		problems, err := account(s, root, p, *updateSchema)
		if err != nil {
			t.Fatalf("%s: %v", provider, err)
		}
		if *updateSchema {
			if err := providerfields.Save(path, s); err != nil {
				t.Fatal(err)
			}
		}
		if len(problems) > 0 {
			t.Errorf("%s: %d fields are not accounted for in %s:\n%s", provider, len(problems), path, strings.Join(problems, "\n"))
		}
	}
}

func TestAccountFailsWhenTheSchemaLacksAnEntity(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	s := providerfields.Schema{Entities: map[string]*providerfields.Entity{}}
	if _, err := account(s, root, providers["applemusic"], false); err == nil {
		t.Fatal("want an error")
	}
}

func TestAccountFailsWhenTheSchemaHasAnEntityNobodyAudits(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatal(err)
	}
	s := providerfields.Schema{Entities: map[string]*providerfields.Entity{"song": {}, "album": {}, "artist": {}, "playlist": {}}}
	if _, err := account(s, root, providers["applemusic"], false); err == nil {
		t.Fatal("want an error")
	}
}
