package main

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/providerfields"
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

const sourceFetchTimeout = 30 * time.Second

type source interface {
	Reference(ctx context.Context, entity string) (providerfields.Observed, error)
	Resource(ctx context.Context, entity, id string) (json.RawMessage, error)
}

func main() {
	provider := flag.String("provider", "", "provider whose schema to refresh, e.g. applemusic")
	schema := flag.String("schema", "", "schema file to refresh (default docs/providers/schema/<provider>.yaml)")
	envFile := flag.String("env", "", "KEY=VALUE file to read provider keys from, e.g. .env.development")
	flag.Parse()
	p, src, root, err := setup(*provider, *envFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "providerschema:", err)
		os.Exit(2)
	}
	path := cmp.Or(*schema, schemaPath(root, *provider))
	audit := func(s providerfields.Schema) (map[string]providerfields.Reads, error) { return p.reads(root, s) }
	if err := refresh(context.Background(), src, path, audit, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "providerschema:", err)
		os.Exit(1)
	}
}

func setup(name, envFile string) (provider, source, string, error) {
	if envFile != "" {
		if err := loadEnvFile(envFile); err != nil {
			return provider{}, nil, "", err
		}
	}
	p, ok := providers[name]
	if !ok {
		return provider{}, nil, "", fmt.Errorf("unknown -provider %q (known: %v)", name, sortedNames(providers))
	}
	root, err := repoRoot()
	if err != nil {
		return provider{}, nil, "", err
	}
	client, err := outbound.NewClient(sourceFetchTimeout, nil)
	if err != nil {
		return provider{}, nil, "", fmt.Errorf("new client: %w", err)
	}
	src, err := p.source(client)
	return p, src, root, err
}

type auditor func(s providerfields.Schema) (map[string]providerfields.Reads, error)

func refresh(ctx context.Context, src source, path string, audit auditor, out io.Writer) error {
	s, err := providerfields.Load(path)
	if err != nil {
		return fmt.Errorf("call failed: %w", err)
	}
	if err := fetchAndMerge(ctx, src, s, audit); err != nil {
		return err
	}
	if err := providerfields.Save(path, s); err != nil {
		return fmt.Errorf("call failed: %w", err)
	}
	for _, named := range inOrder(s.Entities) {
		report(out, named.name, named.entity)
	}
	return nil
}

func fetchAndMerge(ctx context.Context, src source, s providerfields.Schema, audit auditor) error {
	reads, err := audit(s)
	if err != nil {
		return err
	}
	for _, named := range inOrder(s.Entities) {
		if err := refreshEntity(ctx, src, named.name, named.entity, reads[named.name]); err != nil {
			return err
		}
	}
	return nil
}

func refreshEntity(ctx context.Context, src source, name string, entity *providerfields.Entity, reads providerfields.Reads) error {
	reference, err := src.Reference(ctx, name)
	if err != nil {
		return fmt.Errorf("%s reference: %w", name, err)
	}
	live := providerfields.Observed{}
	for _, fixture := range entity.Fixtures {
		raw, err := src.Resource(ctx, name, fixture.ID)
		if err != nil {
			return fmt.Errorf("%s fixture %s: %w", name, fixture.ID, err)
		}
		if err := providerfields.Observe(raw, live); err != nil {
			return fmt.Errorf("%s fixture %s: %w", name, fixture.ID, err)
		}
	}
	entity.Merge(reference.Collapse(reads.Maps), live.Collapse(reads.Maps))
	entity.Account(reads)
	return nil
}

func report(out io.Writer, name string, entity *providerfields.Entity) {
	var c fieldCounts
	for _, f := range entity.Fields {
		c.add(f)
	}
	_, _ = fmt.Fprintf(out, "%s: %d fields, %d only in the reference, %d only live, %d decoded but never sent, %d read only by key match, %d with no read flag or reason\n",
		name, len(entity.Fields), c.referenceOnly, c.liveOnly, c.neverSent, c.byKey, c.open)
}

type fieldCounts struct {
	referenceOnly, liveOnly, neverSent, byKey, open int
}

func (c *fieldCounts) add(f providerfields.Field) {
	c.addSeen(f.Seen)
	if f.ByKey {
		c.byKey++
	}
	if lacksReason(f) {
		c.open++
	}
}

func (c *fieldCounts) addSeen(seen []string) {
	switch {
	case len(seen) == 0:
		c.neverSent++
	case !slices.Contains(seen, providerfields.SeenLive):
		c.referenceOnly++
	case !slices.Contains(seen, providerfields.SeenReference):
		c.liveOnly++
	}
}

func lacksReason(f providerfields.Field) bool {
	if len(f.Seen) == 0 {
		return f.Absent == ""
	}
	return !f.Read && f.Unused == ""
}

type namedEntity struct {
	name   string
	entity *providerfields.Entity
}

func inOrder(entities map[string]*providerfields.Entity) []namedEntity {
	ordered := make([]namedEntity, 0, len(entities))
	for name, entity := range entities {
		ordered = append(ordered, namedEntity{name: name, entity: entity})
	}
	slices.SortFunc(ordered, func(a, b namedEntity) int { return strings.Compare(a.name, b.name) })
	return ordered
}

func sortedNames[V any](entries map[string]V) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
